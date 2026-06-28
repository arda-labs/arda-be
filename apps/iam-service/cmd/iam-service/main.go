package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/iam-service/internal/audit"
	"github.com/arda-labs/arda/apps/iam-service/internal/auth"
	"github.com/arda-labs/arda/apps/iam-service/internal/bootstrap"
	"github.com/arda-labs/arda/apps/iam-service/internal/config"
	"github.com/arda-labs/arda/apps/iam-service/internal/handler"
	"github.com/arda-labs/arda/apps/iam-service/internal/hydra"
	"github.com/arda-labs/arda/apps/iam-service/internal/kratos"
	"github.com/arda-labs/arda/apps/iam-service/internal/mfa"
	"github.com/arda-labs/arda/apps/iam-service/internal/migration"
	"github.com/arda-labs/arda/apps/iam-service/internal/policy"
	"github.com/arda-labs/arda/apps/iam-service/internal/provider"
	providerpassword "github.com/arda-labs/arda/apps/iam-service/internal/provider/password"
	"github.com/arda-labs/arda/apps/iam-service/internal/ratelimit"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	transport "github.com/arda-labs/arda/apps/iam-service/internal/transport/http"
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

	if err := db.PingContext(context.Background()); err != nil {
		logger.Error("ping database", "err", err)
		os.Exit(1)
	}

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("run migrations", "err", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	if err := bootstrap.EnsureSuperAdmin(context.Background(), db, bootstrap.SuperAdminOptions{
		InitialPassword: cfg.SuperAdminInitialPassword,
		PasswordHash:    cfg.SuperAdminPasswordHash,
	}); err != nil {
		logger.Error("bootstrap superadmin", "err", err)
		os.Exit(1)
	}
	logger.Info("superadmin bootstrap reconciled")

	// ── Repositories ──
	userRepo := repository.NewUserRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	sessionRepo := repository.NewSessionRepository(db)
	mfaRepo := repository.NewMFARepository(db)

	// ── Hydra client ──
	hydraClient := hydra.New(cfg.HydraAdminURL, cfg.HydraPublicURL)

	// ── Kratos Admin client ──
	kratosClient := kratos.New(cfg.KratosAdminURL)

	// ── Audit logger (uses chain-hash DB writer) ──
	auditLogger := audit.New("iam-service", auditRepo)

	// ── Rate limiter ──
	limiter := ratelimit.New()

	// ── Provider registry ──
	registry := provider.NewRegistry()
	internalProvider := providerpassword.New(userRepo)
	if err := registry.Register(internalProvider); err != nil {
		logger.Error("register internal provider", "err", err)
		os.Exit(1)
	}
	if err := registry.ValidateAll(context.Background()); err != nil {
		logger.Error("validate providers", "err", err)
		os.Exit(1)
	}
	logger.Info("providers registered", "count", len(registry.ListEnabled()), "ids", providerIDs(registry))

	// ── Casbin enforcer ──
	var policyEnf *policy.Enforcer
	modelPath := "config/casbin_model.conf"
	if _, err := os.Stat(modelPath); err == nil {
		casbinAdapter := policy.NewPostgresAdapter(db)
		enf, err := policy.NewEnforcer(modelPath, casbinAdapter)
		if err != nil {
			logger.Warn("casbin enforcer not available", "err", err)
		} else {
			policyEnf = enf
			logger.Info("casbin enforcer loaded (postgres)")
		}
	} else {
		logger.Warn("casbin model not found, policy enforcement disabled", "path", modelPath)
	}

	// ── Services ──
	sessionSvc := service.NewSessionService(sessionRepo, service.DefaultSessionConfig)
	totpSvc := mfa.New(cfg.TOTPIssuer)
	mfaSvc := service.NewMFAService(mfaRepo, sessionSvc, totpSvc, service.DefaultMFAConfig)
	auditSvc := service.NewAuditService(auditRepo, service.DefaultAuditConfig)

	// ── Auth orchestrator (with MFA) ──
	orchestrator := auth.NewOrchestrator(
		registry, hydraClient, userRepo, policyEnf,
		limiter, auditLogger, cfg.HydraClientID, cfg.HydraRedirectURI,
	).WithMFAService(mfaSvc)

	// ── Handlers ──
	userSvc := service.NewUserService(userRepo, kratosClient)
	userHandler := handler.NewUserHandler(userSvc)
	authHandler := handler.NewAuthHandler(orchestrator, userHandler)
	policyHandler := handler.NewPolicyHandler(policyEnf)
	adminHandler := handler.NewAdminHandler(userRepo, roleRepo, kratosClient)
	sessionHandler := handler.NewSessionHandler(sessionSvc)
	mfaHandler := handler.NewMFAHandler(mfaSvc)
	auditHandler := handler.NewAuditHandler(auditSvc)

	// ── HTTP server ──
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(userHandler, authHandler, policyHandler, adminHandler, sessionHandler, mfaHandler, auditHandler),
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
	switch level {
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

func providerIDs(registry *provider.Registry) []string {
	providers := registry.ListEnabled()
	ids := make([]string, len(providers))
	for i, p := range providers {
		ids[i] = p.ID
	}
	return ids
}
