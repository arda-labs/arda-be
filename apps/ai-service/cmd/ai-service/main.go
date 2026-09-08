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

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/ai-service/internal/catalog"
	"github.com/arda-labs/arda/apps/ai-service/internal/config"
	"github.com/arda-labs/arda/apps/ai-service/internal/events"
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
	if cfg.RAGEmbeddingDimensions != 1024 {
		logger.Error("AI_RAG_EMBEDDING_DIMENSIONS must be 1024 for the current schema", "dimensions", cfg.RAGEmbeddingDimensions)
		os.Exit(1)
	}
	var db *sql.DB
	var store *repository.SQLRunStore
	if cfg.DatabaseDSN != "" {
		var err error
		db, err = sql.Open("pgx/v5", cfg.DatabaseDSN)
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

	// Model configuration is tenant-managed: production always resolves the
	// provider from ai_tenant_settings (AI Settings UI; API keys encrypted at
	// rest via ARDA_SERVICE_AUTH_SECRET) and fails closed when a tenant has
	// no active configuration. The deployment env model is a development
	// fallback for local runs without a database only.
	var ModelProvider model.Provider
	if cfg.Mode != "production" {
		if cfg.ModelReady() {
			client := model.NewClient(cfg.ModelBaseURL, cfg.ModelAPIKey, cfg.ModelID, nil)
			if cfg.ModelGatewayToken != "" {
				client.WithGatewayToken(cfg.ModelGatewayToken)
			}
			ModelProvider = model.NewCircuitBreakerProvider(client, 3, 30*time.Second)
		} else if cfg.ModelEnabled {
			logger.Warn("model provider is not configured; tenants must provide an active model setting")
		}
	} else if cfg.ModelEnabled {
		logger.Info("model provider is tenant-managed; configure it per tenant in AI Settings")
	}

	var knowledgeSvc *knowledge.Service
	var inProcessRAG *knowledge.InProcessRAGAdapter
	if db != nil {
		knowledgeRepo := knowledge.NewRepository(db)
		var embedder knowledge.Embedder
		if cfg.RAGEmbeddingBaseURL != "" {
			embedder = knowledge.NewOpenAIEmbedder(cfg.RAGEmbeddingBaseURL, cfg.RAGEmbeddingAPIKey, cfg.RAGEmbeddingModel, cfg.RAGEmbeddingDimensions, nil)
		}
		knowledgeSvc = knowledge.NewService(knowledgeRepo, embedder, logger)
		knowledgeSvc.SetRequireEmbedding(cfg.RAGRequireEmbedding)
		if cfg.RAGRerankerBaseURL != "" {
			knowledgeSvc.SetReranker(knowledge.NewCohereReranker(cfg.RAGRerankerBaseURL, cfg.RAGRerankerAPIKey, cfg.RAGRerankerModel, nil))
		}
		go knowledgeSvc.StartWorker(context.Background())
		inProcessRAG = knowledge.NewInProcessRAGAdapter(knowledgeSvc)
	}

	var ragClient *svcclient.RAGClient
	if cfg.RAGServiceURL != "" {
		ragClient = svcclient.NewRAGClient(cfg.RAGServiceURL, "ai-service", cfg.ServiceAuthSecret, nil)
	}

	var eventPublisher events.Publisher
	if cfg.NATSURL != "" {
		natsPub, err := events.NewNATSPublisher(cfg.NATSURL, cfg.AppName, logger)
		if err != nil {
			logger.Warn("could not connect to NATS; falling back to buffered publisher", "nats_url", cfg.NATSURL, "err", err)
			eventPublisher = events.NewBufferedPublisher(1000, logger)
		} else {
			eventPublisher = natsPub
			logger.Info("AI service NATS JetStream event publisher started", "nats_url", cfg.NATSURL, "stream", events.StreamName)
		}
	} else {
		eventPublisher = events.NewBufferedPublisher(1000, logger)
	}
	defer eventPublisher.Close()

	var providerRegistry *model.ProviderRegistry
	if cfg.ProvidersConfigFile != "" {
		if reg, err := model.LoadProvidersFromYAML(cfg.ProvidersConfigFile); err == nil {
			providerRegistry = reg
			go providerRegistry.StartActiveHealthProbing(context.Background())
			logger.Info("multi-provider registry loaded from file", "config_file", cfg.ProvidersConfigFile)
		}
	}

	routerOptions := handler.RouterOptions{
		EnableHITLProposals:   cfg.EnableHITLProposals,
		ModelProvider:         ModelProvider,
		ModelPool:             model.NewClientPool(nil),
		AgentMaxSteps:         cfg.AgentMaxSteps,
		ModelSystemPrompt:     cfg.ModelSystemPrompt,
		ModelBaseURLAllowlist: cfg.ModelBaseURLAllowlist,
		AllowLocalModelURLs:   cfg.Mode != "production",
		PlatformModelBaseURL:  cfg.ModelBaseURL,
		PlatformModelID:       cfg.ModelID,
		RAGService:            knowledgeSvc,
		EventPublisher:        eventPublisher,
		ProviderRegistry:      providerRegistry,
	}
	if db != nil || (cfg.ModelEnabled && cfg.ModelReady()) {
		routerOptions.ReadyCheck = func(ctx context.Context) error {
			if db != nil {
				if err := db.PingContext(ctx); err != nil {
					return err
				}
			}
			if cfg.ModelEnabled && cfg.ModelReady() && ModelProvider != nil {
				if prober, ok := ModelProvider.(model.Prober); ok {
					probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
					defer cancel()
					probeErr := prober.Probe(probeCtx)
					providerName := "unknown"
					if descriptor, ok := ModelProvider.(interface{ ProviderName() string }); ok {
						providerName = descriptor.ProviderName()
					}
					handler.RecordProviderProbe(providerName, probeErr)
					if probeErr != nil {
						return probeErr
					}
				}
			}
			return nil
		}
	} else if cfg.Mode == "production" {
		routerOptions.ReadyCheck = func(context.Context) error { return errors.New("database is required in production") }
	} else if cfg.ModelEnabled && !cfg.ModelReady() {
		routerOptions.ReadyCheck = func(context.Context) error { return errors.New("model provider is not configured") }
	}
	if inProcessRAG != nil {
		routerOptions.RAGClient = inProcessRAG
	} else {
		routerOptions.RAGClient = ragClient
	}

	var resolver *tools.Registry
	if cfg.EnableReadTools {
		// Code Mode: Expose ONLY the meta-tools (search & execute & readResult)
		// to the model. Domain APIs are dispatched internally through the
		// embedded Goja sandbox via typed clients with signed caller identity
		// and delegated subject. Raw results stay in the sandbox store; the
		// model fetches full output via readResult.
		var ragSearcher catalog.RAGSearcher
		if inProcessRAG != nil {
			ragSearcher = inProcessRAG
		} else if ragClient != nil {
			ragSearcher = ragClient
		}
		suite := catalog.NewCodeModeSuite(
			svcclient.NewCRMClient(cfg.CRMServiceURL, "ai-service", cfg.ServiceAuthSecret, nil),
			svcclient.NewFinanceClient(cfg.FinanceServiceURL, "ai-service", cfg.ServiceAuthSecret, nil),
			svcclient.NewHRMClient(cfg.HRMServiceURL, "ai-service", cfg.ServiceAuthSecret, nil),
			svcclient.NewIAMClient(cfg.IAMServiceURL, "ai-service", cfg.ServiceAuthSecret, nil),
			store, cfg.EnableHITLProposals, ragSearcher,
		)
		if eventPublisher != nil {
			suite.SetEventPublisher(eventPublisher)
		}
		// readResult is model-visible so the agent can fetch full sandbox
		// outputs by resultId when the inline preview is truncated.
		resolver = tools.NewRegistry(suite.SearchTool, suite.ExecuteTool, suite.ReadTool)
		routerOptions.ModelSDKTypes = suite.TypeDefs

		if suite.Registry != nil {
			entries := suite.Registry.AllEntries()
			toolsDTO := make([]handler.CatalogToolDTO, 0, len(entries))
			for _, e := range entries {
				toolsDTO = append(toolsDTO, handler.CatalogToolDTO{
					MethodName:          e.MethodName,
					SDKPath:             e.SDKPath,
					Domain:              e.Domain,
					Signature:           e.Signature,
					JSDoc:               e.JSDoc,
					Keywords:            e.Keywords,
					Kind:                e.Kind,
					RequiredPermissions: e.RequiredPermissions,
					Risk:                e.Risk,
					TimeoutMs:           e.Timeout.Milliseconds(),
				})
			}
			routerOptions.CatalogTools = toolsDTO
		}
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
		logger.Error("AI service stopped unexpectedly", "err", err)
		os.Exit(1)
	}
	logger.Info("AI service stopped gracefully")
}
