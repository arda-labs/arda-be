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
	"github.com/arda-labs/arda/apps/ai-service/internal/rewrite"
	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	ardaredis "github.com/arda-labs/arda/libs/go/arda-redis"
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
		ardapostgres.ConfigureDefaultPool(db, logger)
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
		knowledgeSvc.SetMinSimilarity(cfg.RAGMinSimilarity)
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

	// Model configuration is tenant-owned (AI Settings UI); the deployment
	// supplies only the shared gateway token and base-URL allowlist.
	modelPool := model.NewClientPool(nil)
	modelPool.SetGatewayToken(cfg.ModelGatewayToken)
	if knowledgeSvc != nil && cfg.RAGQueryRewrite {
		knowledgeSvc.SetQueryRewriter(rewrite.New(store, modelPool, cfg.ServiceAuthSecret))
	}

	routerOptions := handler.RouterOptions{
		EnableHITLProposals:   cfg.EnableHITLProposals,
		ModelPool:             modelPool,
		AgentMaxSteps:         cfg.AgentMaxSteps,
		ModelSystemPrompt:     cfg.ModelSystemPrompt,
		ModelBaseURLAllowlist: cfg.ModelBaseURLAllowlist,
		ModelGatewayToken:     cfg.ModelGatewayToken,
		ModelSessionSecret:    cfg.ServiceAuthSecret,
		AllowLocalModelURLs:   cfg.Mode != "production",
		AgentRunTimeout:       cfg.AgentRunTimeout,
		RAGService:            knowledgeSvc,
		EventPublisher:        eventPublisher,
	}
	routerOptions.ReadyCheck = func(ctx context.Context) error {
		if db != nil {
			if err := db.PingContext(ctx); err != nil {
				return err
			}
		} else if cfg.Mode == "production" {
			return errors.New("database is required in production")
		}
		if cfg.RAGRequireEmbedding && cfg.RAGEmbeddingBaseURL == "" {
			return errors.New("embedding provider is required but not configured")
		}
		return nil
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
		//
		// Tool governance (ADR-003): platform-level runtime overrides on top
		// of the contract defaults. Without a database the overrides are
		// unavailable and every entry follows its contract default.
		governance := catalog.NewGovernance(nil)
		if store != nil {
			governance = catalog.NewGovernance(store)
		}
		routerOptions.ToolGovernance = governance

		var ragSearcher catalog.RAGSearcher
		if inProcessRAG != nil {
			ragSearcher = inProcessRAG
		} else if ragClient != nil {
			ragSearcher = ragClient
		}
		serviceClients := catalog.ClientSet(svcclient.NewServiceClients(cfg.ServiceURLs, "ai-service", cfg.ServiceAuthSecret, nil))
		suite := catalog.NewCodeModeSuite(
			serviceClients,
			store, cfg.EnableHITLProposals, ragSearcher,
			catalog.NewHTTPDocsLookuper(cfg.ProblemDocsURL),
		)
		suite.SetGovernance(governance)
		// Fail closed: a contract service without a base URL means its tools
		// would silently disappear. Production refuses to start; other
		// environments log the gap loudly.
		if len(suite.UnwiredServices) > 0 {
			if cfg.Mode == "production" {
				logger.Error("generated AI tools reference services without a configured base URL",
					"services", suite.UnwiredServices,
					"hint", "set <SERVICE>_SERVICE_URL in arda-infra/k8s/apps/ai-service.yaml")
				os.Exit(1)
			}
			logger.Warn("generated AI tools skipped: service base URL not configured", "services", suite.UnwiredServices)
		}
		if eventPublisher != nil {
			suite.SetEventPublisher(eventPublisher)
		}
		// readResult is model-visible so the agent can fetch full sandbox
		// outputs by resultId when the inline preview is truncated.
		resolver = tools.NewRegistry(suite.SearchTool, suite.ExecuteTool, suite.ReadTool)
		// Evaluated per run: a runtime disable disappears from the model
		// context without a restart (ADR-003).
		routerOptions.ModelSDKTypesProvider = suite.TypeDefinitions

		if suite.Registry != nil {
			entries := suite.Registry.AllEntries()
			toolsDTO := make([]handler.CatalogToolDTO, 0, len(entries))
			for _, e := range entries {
				toolsDTO = append(toolsDTO, handler.CatalogToolDTO{
					MethodName:          e.MethodName,
					SDKPath:             e.SDKPath,
					Domain:              e.Domain,
					Service:             e.Service,
					Signature:           e.Signature,
					JSDoc:               e.JSDoc,
					Keywords:            e.Keywords,
					Kind:                e.Kind,
					RequiredPermissions: e.RequiredPermissions,
					Risk:                e.Risk,
					TimeoutMs:           e.Timeout.Milliseconds(),
					ContractEnabled:     e.Enabled,
					Source:              "internal",
				})
			}
			routerOptions.CatalogTools = toolsDTO
			logger.Info("code mode catalog registered",
				"entries", len(entries),
				"services", len(serviceClients),
				"unwired_services", suite.UnwiredServices,
			)
		}
		routerOptions.ApprovalResolver = catalog.NewExecutionResolver(suite.Registry)
		// The FE-initiated proposal allowlist only covers tools the contract
		// enables; runtime overrides are re-checked per request.
		routerOptions.ProposalTools = proposalToolSpecs(suite.Registry.EnabledEntries())
	}

	mux := handler.NewRouterWithOptions(store, resolver, routerOptions)
	var rateLimitStore handler.RateLimitStore
	if cfg.RedisURL != "" {
		rdb, err := ardaredis.Connect(context.Background(), cfg.RedisURL)
		if err != nil {
			logger.Warn("redis rate limiter unavailable; using the in-process limiter", "err", err)
		} else {
			defer rdb.Close()
			rateLimitStore = handler.NewRedisRateLimitStore(rdb, cfg.RateLimitPerMinute)
			logger.Info("rate limiter: redis")
		}
	}
	handlerChain := ardahttp.MetricsMiddleware(cfg.AppName, ardahttp.UserTimezoneMiddleware(handler.ServiceAuthMiddleware(
		handler.RateLimitMiddleware(mux, cfg.RateLimitPerMinute, rateLimitStore),
		cfg.ServiceAuthSecret,
		cfg.Mode == "production",
	)), handler.RenderAIMetrics)

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
	)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("AI service stopped unexpectedly", "err", err)
		os.Exit(1)
	}
	logger.Info("AI service stopped gracefully")
}

// proposalToolSpecs builds the FE-initiated proposal allowlist from the
// registered catalog: only confirm-kind tools can be proposed, and the
// permission gating their execution travels with the spec.
func proposalToolSpecs(entries []catalog.CatalogEntry) []handler.ProposalToolSpec {
	specs := make([]handler.ProposalToolSpec, 0)
	for _, entry := range entries {
		if entry.Kind != "confirm" {
			continue
		}
		permission := ""
		if len(entry.RequiredPermissions) > 0 {
			permission = entry.RequiredPermissions[0]
		}
		specs = append(specs, handler.ProposalToolSpec{
			Name:               entry.MethodName,
			Version:            1,
			Risk:               entry.Risk,
			RequiredPermission: permission,
		})
	}
	return specs
}
