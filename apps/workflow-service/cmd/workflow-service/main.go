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

	"github.com/arda-labs/arda/apps/workflow-service/internal/config"
	"github.com/arda-labs/arda/apps/workflow-service/internal/handler"
	"github.com/arda-labs/arda/apps/workflow-service/internal/migration"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	transport "github.com/arda-labs/arda/apps/workflow-service/internal/transport/http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
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

	// Zeebe Client
	zeebeSvc, err := service.NewZeebeService(cfg.ZeebeAddr)
	if err != nil {
		logger.Error("Failed to connect to Zeebe gateway", "err", err)
		os.Exit(1)
	}
	defer zeebeSvc.Close()

	// Repositories
	mappingRepo := repository.NewMappingRepository(db)
	caseRepo := repository.NewCaseRepository(db)
	processDefinitionRepo := repository.NewProcessDefinitionRepository(db)

	// Handlers
	wfHandler := handler.NewWorkflowHandler(zeebeSvc, mappingRepo, caseRepo, processDefinitionRepo)

	// Router and HTTP Server
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(wfHandler),
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
