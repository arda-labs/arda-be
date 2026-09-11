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

	"net"

	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/arda-labs/arda/apps/finance-service/internal/config"
	"github.com/arda-labs/arda/apps/finance-service/internal/handler"
	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	grpcserver "github.com/arda-labs/arda/apps/finance-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/finance-service/internal/transport/http"
	"github.com/nats-io/nats.go"

	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// loanMetricsAdapter adapts the loan gRPC client to service.ExternalMetricsProvider.
type loanMetricsAdapter struct {
	client *loanclient.Client
}

func (a loanMetricsAdapter) OperationMetrics(ctx context.Context, tenantID, fromDate, toDate string) (int64, int64, error) {
	resp, err := a.client.GetOperationMetrics(ctx, tenantID, fromDate, toDate)
	if err != nil {
		return 0, 0, err
	}
	return resp.GetCollectionVolumeMinor(), resp.GetNplBalanceMinor(), nil
}

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	// ── Database ──
	db, err := sql.Open("pgx/v5", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	ardapostgres.ConfigureDefaultPool(db, logger)

	if err := db.PingContext(context.Background()); err != nil {
		logger.Error("ping database", "err", err)
		os.Exit(1)
	}

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("run migrations", "err", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	// ── Repositories ──
	accountRepo := repository.NewAccountRepository(db)
	configRepo := repository.NewConfigRepository(db)
	coaRepo := repository.NewCoaRepository(db)

	// ── Workflow client (manual posting cases) ──
	workflow, err := workflowclient.Dial(context.Background(), cfg.WorkflowGRPCAddr, cfg.AppName, logger)
	if err != nil {
		logger.Error("workflow grpc dial failed", "err", err)
		os.Exit(1)
	}
	defer workflow.Close()

	// ── Loan client (TT92 external statement metrics; optional) ──
	loan, loanErr := loanclient.Dial(context.Background(), cfg.LoanGRPCAddr, cfg.AppName, logger)
	if loanErr != nil {
		logger.Warn("loan grpc dial failed; external statement metrics unavailable", "err", loanErr)
	} else {
		defer loan.Close()
	}

	// ── Services ──
	accountSvc := service.NewAccountService(accountRepo)
	trialBalanceSvc := service.NewTrialBalanceService(db)
	accountingConfigSvc := service.NewAccountingConfigService(configRepo)
	coaSvc := service.NewCoaService(coaRepo)
	postingRepo := repository.NewPostingRepository(db)
	postingSvc := service.NewPostingService(postingRepo, db)
	cashSvc := service.NewCashService(db, postingSvc)
	postingCaseSvc := service.NewPostingCaseService(postingSvc, workflow)
	trialBalanceDailySvc := service.NewTrialBalanceDailyService(db)
	statementSvc := service.NewStatementService(db)
	if loan != nil {
		statementSvc.SetExternalMetrics(loanMetricsAdapter{client: loan})
	}

	// ── Handlers ──
	financeHandler := handler.NewFinanceHandler(accountSvc, trialBalanceSvc, accountingConfigSvc, cashSvc)
	coaHandler := handler.NewCoaHandler(coaSvc)
	postingHandler := handler.NewPostingHandler(postingSvc)
	cashHandler := handler.NewCashHandler(cashSvc)
	postingCaseHandler := handler.NewPostingCaseHandler(postingCaseSvc)
	reportingHandler := handler.NewReportingHandler(trialBalanceDailySvc, statementSvc)
	counterpartySvc := service.NewCounterpartyService(configRepo)
	counterpartyHandler := handler.NewCounterpartyHandler(counterpartySvc)

	// ── gRPC server (PostingService, port 9090) ──
	serviceSecret, err := identity.SecretFromEnv()
	if err != nil {
		logger.Error("service identity is not configured", "err", err)
		os.Exit(1)
	}
	transportCreds, err := identity.ServerTransportCredentials()
	if err != nil {
		logger.Error("grpc transport credentials", "err", err)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer(
		grpc.Creds(transportCreds),
		grpc.ChainUnaryInterceptor(
			interceptors.UnaryServerServiceAuth(serviceSecret, "finance-service", map[string]struct{}{"workflow-service": {}, "deposit-service": {}, "capital-service": {}}),
			interceptors.UnaryServerLogging(logger),
		),
	)
	financev1.RegisterPostingServiceServer(grpcSrv, grpcserver.NewPostingServer(postingSvc))
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	if conn, err := nats.Connect(cfg.NATSURL); err != nil {
		logger.Warn("outbox relay disabled: nats unavailable", "err", err)
	} else {
		defer conn.Close()
		relay := service.NewOutboxRelay(db, conn, logger)
		go relay.Run(appCtx)
	}

	go func() {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			logger.Error("grpc listen", "err", err)
			os.Exit(1)
		}
		logger.Info("grpc server started", "name", cfg.AppName, "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("grpc server error", "err", err)
		}
	}()
	defer grpcSrv.GracefulStop()

	// ── HTTP server ──
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, ardahttp.UserTimezoneMiddleware(transport.NewRouter(financeHandler, coaHandler, postingHandler, cashHandler, postingCaseHandler, reportingHandler, counterpartyHandler))),
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
