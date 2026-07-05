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

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/crm-service/internal/config"
	"github.com/arda-labs/arda/apps/crm-service/internal/handler"
	"github.com/arda-labs/arda/apps/crm-service/internal/migration"
	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	grpcserver "github.com/arda-labs/arda/apps/crm-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/crm-service/internal/transport/http"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	crmv1 "github.com/arda-labs/arda/libs/go/arda-proto/crm/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	// Database Connection
	db, err := sql.Open("postgres", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("Failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	ardapostgres.ConfigureDefaultPool(db, logger)

	if err := db.PingContext(context.Background()); err != nil {
		logger.Error("Failed to ping database", "err", err)
		os.Exit(1)
	}

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("Failed to run migrations", "err", err)
		os.Exit(1)
	}
	logger.Info("Database migrations applied successfully")

	// Repositories
	customerRepo := repository.NewCustomerRepository(db)
	amendmentRepo := repository.NewAmendmentRepository(db)

	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(interceptors.UnaryServerLogging(logger)))
	crmv1.RegisterCustomerCommandServiceServer(grpcSrv, grpcserver.NewCustomerCommandServer(customerRepo, amendmentRepo))
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
			os.Exit(1)
		}
	}()

	workflowClient, err := workflowclient.Dial(context.Background(), cfg.WorkflowGRPCAddr, cfg.AppName, logger)
	if err != nil {
		logger.Error("workflow grpc unavailable", "addr", cfg.WorkflowGRPCAddr, "err", err)
		os.Exit(1)
	}
	defer workflowClient.Close()
	logger.Info("workflow grpc configured", "addr", cfg.WorkflowGRPCAddr)

	// Handlers
	customerHandler := handler.NewCustomerHandler(customerRepo, workflowClient)
	amendmentHandler := handler.NewAmendmentHandler(customerRepo, amendmentRepo, workflowClient)

	// Router and HTTP Server
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(customerHandler, amendmentHandler),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("Service started", "name", cfg.AppName, "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down service", "name", cfg.AppName)
	grpcSrv.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown error", "err", err)
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
