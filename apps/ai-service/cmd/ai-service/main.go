package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/ai-service/internal/config"
	"github.com/arda-labs/arda/apps/ai-service/internal/handler"
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/migration"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()
	var db *sql.DB
	var store *repository.SQLRunStore
	if cfg.DatabaseDSN != "" {
		var err error
		db, err = sql.Open("postgres", cfg.DatabaseDSN)
		if err != nil {
			logger.Error("failed to open AI database", "err", err)
			os.Exit(1)
		}
		defer db.Close()
		db.SetMaxOpenConns(cfg.DBMaxOpenConns)
		db.SetMaxIdleConns(cfg.DBMaxIdleConns)
		db.SetConnMaxIdleTime(time.Duration(cfg.DBConnMaxIdleSec) * time.Second)
		if err := db.PingContext(context.Background()); err != nil {
			logger.Error("failed to ping AI database", "err", err)
			os.Exit(1)
		}
		if err := migration.Run(db, "postgres"); err != nil {
			logger.Error("failed to apply AI migrations", "err", err)
			os.Exit(1)
		}
		store = repository.NewSQLRunStore(db)
	} else if cfg.Mode == "production" {
		logger.Error("DATABASE_DSN is required in production mode")
		os.Exit(1)
	}
	if cfg.Mode == "production" && len(cfg.ServiceAuthSecret) < 32 {
		logger.Error("ARDA_SERVICE_AUTH_SECRET is required in production mode")
		os.Exit(1)
	}
	var resolver *tools.Registry
	if cfg.EnableReadTools {
		items := make([]tools.Tool, 0, 2)
		if cfg.CRMServiceURL != "" {
			items = append(items, tools.NewCRMCustomerGetTool(cfg.CRMServiceURL, nil))
		}
		if db != nil {
			items = append(items, tools.NewKnowledgeSearchTool(knowledge.NewSQLSearcher(db)))
		}
		if len(items) > 0 {
			resolver = tools.NewRegistry(items...)
		}
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: ardahttp.MetricsMiddleware(cfg.AppName, handler.ServiceAuthMiddleware(
			handler.NewRouterWithDependencies(store, resolver),
			cfg.ServiceAuthSecret,
			cfg.Mode == "production",
		)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("AI service started", "addr", cfg.HTTPAddr, "mode", cfg.Mode, "persistent", store != nil, "read_tools", resolver != nil)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("AI protocol spike stopped", "err", err)
		os.Exit(1)
	}
}
