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

	"github.com/arda-labs/arda/apps/ai-service/internal/catalog"
	"github.com/arda-labs/arda/apps/ai-service/internal/config"
	"github.com/arda-labs/arda/apps/ai-service/internal/handler"
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/migration"
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/sandbox"
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
		items := make([]tools.Tool, 0, 5)
		var crmGetTool *tools.CRMCustomerGetTool
		var crmExportTool *tools.CRMCustomerExportPrepareTool
		var knowledgeTool *tools.KnowledgeSearchTool

		if cfg.CRMServiceURL != "" {
			client := &http.Client{Timeout: 5 * time.Second}
			crmGetTool = tools.NewCRMCustomerGetTool(cfg.CRMServiceURL, client)
			crmExportTool = tools.NewCRMCustomerExportPrepareTool(cfg.CRMServiceURL, client)
			items = append(items, crmGetTool, crmExportTool)
		}
		if db != nil {
			knowledgeTool = tools.NewKnowledgeSearchTool(knowledge.NewSQLSearcher(db))
			items = append(items, knowledgeTool)
		}

		// Initialize Code Mode (search & execute meta-tools via Goja sandbox)
		if cfg.EnableCodeMode {
			dispatcherReg := catalog.NewDispatcherRegistry()
			catalog.RegisterBuiltinCatalog(dispatcherReg, crmGetTool, crmExportTool, knowledgeTool)
			catalogIndex := catalog.NewIndex(dispatcherReg.AllEntries())
			sandboxEngine := sandbox.NewEngine(dispatcherReg)

			searchMetaTool := tools.NewSearchMetaTool(func(query, domain string, scope tools.Context) (string, int, error) {
				entries := catalogIndex.Search(query, domain, scope, 5)
				return catalog.FormatSignatures(entries), len(entries), nil
			})
			executeMetaTool := tools.NewExecuteMetaTool(func(ctx context.Context, scope tools.Context, code string) (map[string]any, error) {
				res, err := sandboxEngine.Execute(ctx, scope, code)
				if err != nil {
					return nil, err
				}
				out := map[string]any{
					"output":        res.Output,
					"durationMs":    res.DurationMs,
					"methodsCalled": res.MethodsCalled,
					"scriptHash":    res.ScriptHash,
				}
				if res.ApprovalNeeded {
					out["approval"] = map[string]any{
						"tool":   res.ProposalTool,
						"args":   res.ProposalArgs,
						"status": "PENDING",
					}
				}
				return out, nil
			})

			// Add meta-tools to registry
			items = append(items, searchMetaTool, executeMetaTool)
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
