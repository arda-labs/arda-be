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
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/arda-labs/arda/apps/hrm-service/internal/config"
	"github.com/arda-labs/arda/apps/hrm-service/internal/handler"
	"github.com/arda-labs/arda/apps/hrm-service/internal/migration"
	"github.com/arda-labs/arda/apps/hrm-service/internal/repository"
	grpcserver "github.com/arda-labs/arda/apps/hrm-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/hrm-service/internal/transport/http"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	hrmv1 "github.com/arda-labs/arda/libs/go/arda-proto/hrm/v1"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	db, err := sql.Open("pgx/v5", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(context.Background()); err != nil {
		logger.Error("ping database", "err", err)
		os.Exit(1)
	}
	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("run migrations", "err", err)
		os.Exit(1)
	}

	workflowClient, err := workflowclient.Dial(context.Background(), cfg.WorkflowGRPCAddr, cfg.AppName, logger)
	if err != nil {
		logger.Error("workflow grpc unavailable", "addr", cfg.WorkflowGRPCAddr, "err", err)
		os.Exit(1)
	}
	defer workflowClient.Close()
	logger.Info("workflow grpc configured", "addr", cfg.WorkflowGRPCAddr)

	hrmHandler := handler.NewHRMHandler(repository.NewHRMRepository(db), workflowClient)
	// ── gRPC server (EmployeeCommandService, port 9090) ──
	serviceSecret, err2 := identity.SecretFromEnv()
	if err2 != nil {
		logger.Error("service identity is not configured", "err", err2)
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
			interceptors.UnaryServerServiceAuth(serviceSecret, "hrm-service", map[string]struct{}{"workflow-service": {}}),
			interceptors.UnaryServerLogging(logger),
		),
	)
	hrmv1.RegisterEmployeeCommandServiceServer(grpcSrv, grpcserver.NewEmployeeServer(db))
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
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, transport.NewRouter(hrmHandler)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("http server started", "name", cfg.AppName, "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "err", err)
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
