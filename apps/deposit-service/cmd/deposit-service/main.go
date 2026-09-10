package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	appconfig "github.com/arda-labs/arda/apps/deposit-service/internal/config"
	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/apps/deposit-service/internal/migration"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	grpcserver "github.com/arda-labs/arda/apps/deposit-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/deposit-service/internal/transport/http"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	depositv1 "github.com/arda-labs/arda/libs/go/arda-proto/deposit/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := loadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))

	db, err := sql.Open("pgx/v5", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("Failed to run migrations", "err", err)
		os.Exit(1)
	}
	logger.Info("Database migrations applied successfully")

	var financeClient *financeclient.Client
	if cfg.FinanceGRPCAddr != "" {
		fc, err := financeclient.Dial(context.Background(), cfg.FinanceGRPCAddr, "deposit-service", logger)
		if err != nil {
			logger.Error("finance grpc dial", "err", err)
			os.Exit(1)
		}
		defer fc.Close()
		financeClient = fc
	}

	repo := repository.NewDepositRepository(db)
	settlementSvc := service.NewSettlementService(repo, db, financeClient)
	depositHandler := handler.NewDepositHandler(settlementSvc)

	// ── gRPC server (DepositCommandService, port 9090) ──
	serviceSecret, errSec := identity.SecretFromEnv()
	if errSec != nil {
		logger.Error("service identity is not configured", "err", errSec)
		os.Exit(1)
	}
	transportCreds, errCreds := identity.ServerTransportCredentials()
	if errCreds != nil {
		logger.Error("grpc transport credentials", "err", errCreds)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer(
		grpc.Creds(transportCreds),
		grpc.ChainUnaryInterceptor(
			interceptors.UnaryServerServiceAuth(serviceSecret, "deposit-service", map[string]struct{}{"workflow-service": {}}),
			interceptors.UnaryServerLogging(logger),
		),
	)
	depositv1.RegisterDepositCommandServiceServer(grpcSrv, grpcserver.NewDepositServer(settlementSvc))
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)

	go func() {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			logger.Error("grpc listen", "err", err)
			os.Exit(1)
		}
		logger.Info("grpc server started", "name", cfg.AppName, "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("grpc server error", "err", err)
		}
	}()
	defer grpcSrv.GracefulStop()

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, ardahttp.UserTimezoneMiddleware(transport.NewRouter(depositHandler))),
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
}

type config struct {
	AppName         string
	HTTPAddr        string
	GRPCAddr        string
	LogLevel        string
	DatabaseDSN     string
	FinanceGRPCAddr string
}

func loadConfig() config {
	base := appconfig.Load()
	return config{
		AppName:         base.AppName,
		HTTPAddr:        base.HTTPAddr,
		GRPCAddr:        envOr("GRPC_ADDR", "0.0.0.0:9090"),
		LogLevel:        base.LogLevel,
		DatabaseDSN:     base.DatabaseDSN,
		FinanceGRPCAddr: base.FinanceGRPCAddr,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
