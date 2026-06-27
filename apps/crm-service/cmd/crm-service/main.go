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

	"github.com/camunda/zeebe/clients/go/v8/pkg/zbc"

	"github.com/arda-labs/arda/apps/crm-service/internal/config"
	"github.com/arda-labs/arda/apps/crm-service/internal/handler"
	"github.com/arda-labs/arda/apps/crm-service/internal/migration"
	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	transport "github.com/arda-labs/arda/apps/crm-service/internal/transport/http"
	"github.com/arda-labs/arda/apps/crm-service/internal/worker"
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

	// Zeebe Client & Job Workers
	zeebeClient, err := zbc.NewClient(&zbc.ClientConfig{
		GatewayAddress:         cfg.ZeebeAddr,
		UsePlaintextConnection: true,
	})
	if err != nil {
		logger.Error("Failed to connect to Zeebe gateway", "err", err)
		os.Exit(1)
	}
	defer zeebeClient.Close()
	logger.Info("Connected to Zeebe gateway", "addr", cfg.ZeebeAddr)

	crmWorkers := worker.NewCRMWorkers(customerRepo)

	// Start Workers
	worker1 := zeebeClient.NewJobWorker().JobType("crm.create_customer").Handler(crmWorkers.CreateCustomerHandler).Open()
	defer worker1.Close()

	worker2 := zeebeClient.NewJobWorker().JobType("crm.update_customer").Handler(crmWorkers.UpdateCustomerHandler).Open()
	defer worker2.Close()

	worker3 := zeebeClient.NewJobWorker().JobType("crm.approve_customer").Handler(crmWorkers.ApproveCustomerHandler).Open()
	defer worker3.Close()

	logger.Info("CRM job workers registered and listening")

	// Handlers
	customerHandler := handler.NewCustomerHandler(customerRepo, cfg.WorkflowServiceURL)

	// Router and HTTP Server
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(customerHandler),
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
