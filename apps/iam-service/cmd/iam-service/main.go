package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/iam-service/internal/audit"
	"github.com/arda-labs/arda/apps/iam-service/internal/bootstrap"
	"github.com/arda-labs/arda/apps/iam-service/internal/config"
	"github.com/arda-labs/arda/apps/iam-service/internal/handler"
	"github.com/arda-labs/arda/apps/iam-service/internal/kratos"
	"github.com/arda-labs/arda/apps/iam-service/internal/mfa"
	"github.com/arda-labs/arda/apps/iam-service/internal/migration"
	"github.com/arda-labs/arda/apps/iam-service/internal/policy"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	iamgrpc "github.com/arda-labs/arda/apps/iam-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/iam-service/internal/transport/http"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardamedia "github.com/arda-labs/arda/libs/go/arda-media"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	// ── Database ──
	db, err := sql.Open("postgres", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	ardapostgres.ConfigureDefaultPool(db, logger)

	if err := db.PingContext(context.Background()); err != nil {
		logger.Error("ping database", "err", err)
		os.Exit(1)
	}

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("run migrations", "err", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	if err := bootstrap.EnsureSuperAdmin(context.Background(), db, bootstrap.SuperAdminOptions{}); err != nil {
		logger.Error("bootstrap superadmin", "err", err)
		os.Exit(1)
	}
	logger.Info("superadmin bootstrap reconciled")

	// ── Repositories ──
	userRepo := repository.NewUserRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	groupRepo := repository.NewGroupRepository(db)
	sessionRepo := repository.NewSessionRepository(db)
	mfaRepo := repository.NewMFARepository(db)

	// ── Kratos Admin client ──
	kratosClient := kratos.New(cfg.KratosAdminURL)
	if strings.Contains(cfg.KratosAdminURL, "localhost") || strings.Contains(cfg.KratosAdminURL, "127.0.0.1") {
		logger.Warn("kratos admin url points to localhost; remote/dev environments may fail user management operations", "kratos_admin_url", cfg.KratosAdminURL)
	}

	// ── Audit logger (uses chain-hash DB writer) ──
	auditLogger := audit.New("iam-service", auditRepo)

	// ── Casbin enforcer ──
	var policyEnf *policy.Enforcer
	modelPath := "config/casbin_model.conf"
	if _, err := os.Stat(modelPath); err != nil {
		logger.Error("casbin model is required; refusing to start", "path", modelPath, "err", err)
		os.Exit(1)
	}
	casbinAdapter := policy.NewPostgresAdapter(db)
	enf, err := policy.NewEnforcer(modelPath, casbinAdapter)
	if err != nil {
		logger.Error("casbin enforcer is required; refusing to start", "err", err)
		os.Exit(1)
	}
	policyEnf = enf
	logger.Info("casbin enforcer loaded (postgres)")

	// ── Services ──
	sessionSvc := service.NewSessionService(sessionRepo, service.DefaultSessionConfig)
	totpSvc := mfa.New(cfg.TOTPIssuer)
	mfaSvc := service.NewMFAService(mfaRepo, sessionSvc, totpSvc, service.DefaultMFAConfig)
	auditSvc := service.NewAuditService(auditRepo, service.DefaultAuditConfig)

	// ── Handlers ──
	identitySvc := service.NewIdentityService(userRepo, kratosClient)
	userSvc := service.NewUserService(userRepo, identitySvc)
	mediaClient, err := ardamedia.NewClient("iam-service")
	if err != nil {
		logger.Error("media grpc client is required; refusing to start", "err", err)
		os.Exit(1)
	}
	defer mediaClient.Close()
	userHandler := handler.NewUserHandler(userSvc, mediaClient)
	policyHandler := handler.NewPolicyHandler(policyEnf)
	adminUserSvc := service.NewAdminUserService(userRepo, roleRepo, identitySvc)
	adminHandler := handler.NewAdminHandler(userRepo, roleRepo, groupRepo, adminUserSvc, auditLogger)
	sessionHandler := handler.NewSessionHandler(sessionSvc, userRepo, auditLogger)
	mfaHandler := handler.NewMFAHandler(mfaSvc, userRepo)
	auditHandler := handler.NewAuditHandler(auditSvc)

	// ── gRPC server ──
	if _, err := iamgrpc.ListenAndServe(cfg.GRPCAddr, userRepo); err != nil {
		logger.Error("start grpc server", "err", err)
		os.Exit(1)
	}
	logger.Info("grpc server started", "addr", cfg.GRPCAddr)

	// ── HTTP server ──
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, transport.NewRouter(userHandler, policyHandler, adminHandler, sessionHandler, mfaHandler, auditHandler)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("service started", "name", cfg.AppName, "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down", "name", cfg.AppName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	auditSvc.Stop()
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
