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

	_ "github.com/jackc/pgx/v5/stdlib"

	appconfig "github.com/arda-labs/arda/apps/statistical-service/internal/config"
	"github.com/arda-labs/arda/apps/statistical-service/internal/handler"
	"github.com/arda-labs/arda/apps/statistical-service/internal/migration"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	"github.com/arda-labs/arda/apps/statistical-service/internal/service"
	transport "github.com/arda-labs/arda/apps/statistical-service/internal/transport/http"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
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

	var workflow StatisticalSubmitterAdapter
	if cfg.WorkflowGRPCAddr != "" {
		wc, err := workflowclient.Dial(context.Background(), cfg.WorkflowGRPCAddr, "statistical-service", logger)
		if err != nil {
			logger.Error("workflow grpc dial", "err", err)
			os.Exit(1)
		}
		defer wc.Close()
		workflow = StatisticalSubmitterAdapter{client: wc}
	}

	repo := repository.NewStatisticalRepository(db)
	statisticalSvc := service.NewStatisticalService(repo, workflow)
	statisticalHandler := handler.NewStatisticalHandler(statisticalSvc)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, transport.NewRouter(statisticalHandler)),
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
	AppName          string
	HTTPAddr         string
	LogLevel         string
	DatabaseDSN      string
	WorkflowGRPCAddr string
}

// StatisticalSubmitterAdapter adapts the workflow client to the service
// submitter interface.
type StatisticalSubmitterAdapter struct {
	client *workflowclient.Client
}

func (a StatisticalSubmitterAdapter) CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	return a.client.CreateCase(ctx, in)
}

func (a StatisticalSubmitterAdapter) SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error) {
	return a.client.SubmitCase(ctx, caseID, actor, variables, idempotencyKey)
}

func loadConfig() config {
	base := appconfig.Load()
	return config{
		AppName:          base.AppName,
		HTTPAddr:         base.HTTPAddr,
		LogLevel:         base.LogLevel,
		DatabaseDSN:      base.DatabaseDSN,
		WorkflowGRPCAddr: base.WorkflowGRPCAddr,
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
