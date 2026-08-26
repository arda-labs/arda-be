package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/ai-service/internal/config"
	"github.com/arda-labs/arda/apps/ai-service/internal/handler"
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/migration"
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
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

	var ModelProvider *model.Client
	if cfg.ModelReady() {
		ModelProvider = model.NewClient(cfg.ModelBaseURL, cfg.ModelAPIKey, cfg.ModelID, nil)
	} else if cfg.ModelEnabled {
		logger.Warn("AI_ENABLE_AGENT is set but model configuration is incomplete; running without the agent loop")
	}

	var resolver *tools.Registry
	if cfg.EnableReadTools {
		items := make([]tools.Tool, 0, 3)
		if cfg.CRMServiceURL != "" {
			client := &http.Client{Timeout: 5 * time.Second}
			items = append(items,
				tools.NewCRMCustomerGetTool(cfg.CRMServiceURL, client),
				tools.NewCRMCustomerExportPrepareTool(cfg.CRMServiceURL, client),
			)
		}
		if db != nil {
			items = append(items, tools.NewKnowledgeSearchTool(knowledge.NewSQLSearcher(db)))
		}
		if len(items) > 0 {
			resolver = tools.NewRegistry(items...)
		}
	}

	routerOptions := handler.RouterOptions{
		EnableHITLProposals: cfg.EnableHITLProposals,
		ModelProvider:         ModelProvider,
		AgentMaxSteps:       cfg.AgentMaxSteps,
		ModelSystemPrompt:   cfg.ModelSystemPrompt,
	}

	mux := handler.NewRouterWithOptions(store, resolver, routerOptions)
	handlerChain := ardahttp.MetricsMiddleware(cfg.AppName, handler.ServiceAuthMiddleware(
		handler.RateLimitMiddleware(mux, cfg.RateLimitPerMinute),
		cfg.ServiceAuthSecret,
		cfg.Mode == "production",
	))

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      handlerChain,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("AI service started",
		"addr", cfg.HTTPAddr, "mode", cfg.Mode,
		"persistent", store != nil, "read_tools", resolver != nil,
		"hitl_proposals", cfg.EnableHITLProposals,
		"agent_model", cfg.ModelReady(),
	)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("AI protocol spike stopped", "err", err)
		os.Exit(1)
	}
	logger.Info("AI service stopped gracefully")
}
