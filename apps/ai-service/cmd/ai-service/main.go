package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/ai-service/internal/catalog"
	"github.com/arda-labs/arda/apps/ai-service/internal/config"
	"github.com/arda-labs/arda/apps/ai-service/internal/handler"
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/migration"
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
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
		store.SetEncryptionSecret(cfg.ServiceAuthSecret)
	} else if cfg.Mode == "production" {
		logger.Error("DATABASE_DSN is required in production mode")
		os.Exit(1)
	}
	if cfg.Mode == "production" && len(cfg.ServiceAuthSecret) < 32 {
		logger.Error("ARDA_SERVICE_AUTH_SECRET is required in production mode")
		os.Exit(1)
	}

	// The env model config is only a fallback for spike/local mode (no
	// database). With persistence, the saved tenant configuration in
	// ai_tenant_settings is the single source of truth (see
	// handler.selectModelProvider) and the env key is ignored.
	var ModelProvider *model.Client
	if cfg.ModelReady() && store == nil {
		ModelProvider = model.NewClient(cfg.ModelBaseURL, cfg.ModelAPIKey, cfg.ModelID, nil)
		if cfg.ModelGatewayToken != "" {
			ModelProvider.WithGatewayToken(cfg.ModelGatewayToken)
		}
	} else if cfg.ModelEnabled {
		logger.Info("model provider comes from tenant settings (database present)",
			"platform_env_key_used", false)
	}

	routerOptions := handler.RouterOptions{
		EnableHITLProposals:   cfg.EnableHITLProposals,
		ModelProvider:         ModelProvider,
		ModelPool:             model.NewClientPool(nil),
		AgentMaxSteps:         cfg.AgentMaxSteps,
		ModelSystemPrompt:     cfg.ModelSystemPrompt,
		ModelBaseURLAllowlist: cfg.ModelBaseURLAllowlist,
		PlatformModelBaseURL:  cfg.ModelBaseURL,
		PlatformModelID:       cfg.ModelID,
	}

	var resolver *tools.Registry
	if cfg.EnableReadTools {
		// Code Mode: Expose ONLY the meta-tools (search & execute & readResult)
		// to the model. Domain APIs are dispatched internally through the
		// embedded Goja sandbox via typed clients with signed caller identity
		// and delegated subject. Raw results stay in the sandbox store; the
		// model fetches full output via readResult.
		suite := catalog.NewCodeModeSuite(
			svcclient.NewCRMClient(cfg.CRMServiceURL, "ai-service", cfg.ServiceAuthSecret, nil),
			svcclient.NewFinanceClient(cfg.FinanceServiceURL, "ai-service", cfg.ServiceAuthSecret, nil),
			svcclient.NewIAMClient(cfg.IAMServiceURL, "ai-service", cfg.ServiceAuthSecret, nil),
			db, store, cfg.EnableHITLProposals, buildKnowledgeEmbedder(cfg, logger),
		)
		// readResult is model-visible so the agent can fetch full sandbox
		// outputs by resultId when the inline preview is truncated.
		resolver = tools.NewRegistry(suite.SearchTool, suite.ExecuteTool, suite.ReadTool)
		routerOptions.ModelSDKTypes = suite.TypeDefs
	}
	if cfg.ModelGatewayToken != "" {
		routerOptions.ModelPool.SetGatewayToken(cfg.ModelGatewayToken)
	}

	mux := handler.NewRouterWithOptions(store, resolver, routerOptions)
	handlerChain := ardahttp.MetricsMiddleware(cfg.AppName, handler.ServiceAuthMiddleware(
		handler.RateLimitMiddleware(mux, cfg.RateLimitPerMinute),
		cfg.ServiceAuthSecret,
		cfg.Mode == "production",
	), handler.RenderAIMetrics)

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

// buildKnowledgeEmbedder wires hybrid knowledge retrieval (roadmap §4.2):
// Cloudflare Workers AI by default, OpenAI-compatible self-host later. A nil
// embedder keeps search full-text-only.
func buildKnowledgeEmbedder(cfg config.Config, logger *slog.Logger) knowledge.Embedder {
	if !cfg.KnowledgeVectorEnabled {
		return nil
	}
	switch strings.ToLower(cfg.EmbeddingProvider) {
	case "openai":
		if cfg.EmbeddingBaseURL != "" && cfg.EmbeddingModel != "" {
			return knowledge.NewOpenAIEmbedder(cfg.EmbeddingBaseURL, cfg.EmbeddingModel, cfg.EmbeddingAPIToken, nil)
		}
	default:
		if cfg.EmbeddingAccountID != "" && cfg.EmbeddingAPIToken != "" && cfg.EmbeddingModel != "" {
			return knowledge.NewWorkersAIEmbedder(cfg.EmbeddingAccountID, cfg.EmbeddingModel, cfg.EmbeddingAPIToken, nil)
		}
	}
	logger.Warn("AI_KNOWLEDGE_VECTOR is set but embedding config is incomplete; knowledge search stays full-text",
		"provider", cfg.EmbeddingProvider)
	return nil
}
