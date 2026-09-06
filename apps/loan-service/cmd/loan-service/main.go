package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/arda-labs/arda/apps/loan-service/internal/config"
	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	grpcserver "github.com/arda-labs/arda/apps/loan-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/loan-service/internal/transport/http"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	loangrpc "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	if cfg.WorkflowGRPCAddr == "" {
		logger.Error("workflow grpc is required; refusing to start")
		os.Exit(1)
	}
	workflow, err := workflowclient.Dial(context.Background(), cfg.WorkflowGRPCAddr, "loan-service", logger)
	if err != nil {
		logger.Error("workflow grpc dial failed", "err", err)
		os.Exit(1)
	}
	defer workflow.Close()

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

	repo := repository.NewLoanRepository(db)
	loanSvc := service.NewLoanService(repo, workflow)
	adjSvc := service.NewAdjustmentService(repo, workflow)
	loanHandler := handler.NewLoanHandler(loanSvc, adjSvc)
	disbSvc := service.NewDisbursementService(repo, workflow)
	disbHandler := handler.NewDisbursementHandler(disbSvc)
	var financeClient *financeclient.Client
	if cfg.FinanceGRPCAddr != "" {
		fc, err := financeclient.Dial(context.Background(), cfg.FinanceGRPCAddr, "loan-service", logger)
		if err != nil {
			logger.Error("finance grpc dial", "err", err)
			os.Exit(1)
		}
		defer fc.Close()
		financeClient = fc
	}

	colSvc := service.NewCollectionService(repo, workflow)
	colHandler := handler.NewCollectionHandler(colSvc)
	accrualSvc := service.NewAccrualService(repo, db, financeClient)
	accrualHandler := handler.NewAccrualHandler(accrualSvc)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, transport.NewRouter(loanHandler, disbHandler, colHandler, accrualHandler, loangrpc.Kinds)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serviceSecret, err := identity.SecretFromEnv()
	if err != nil {
		logger.Error("service identity is not configured", "err", err)
		os.Exit(1)
	}
	transportCreds, err := identity.ServerTransportCredentials()
	if err != nil {
		logger.Error("grpc tls is not configured", "err", err)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer(
		grpc.Creds(transportCreds),
		grpc.ChainUnaryInterceptor(
			interceptors.UnaryServerServiceAuth(serviceSecret, "loan-service", map[string]struct{}{"workflow-service": {}}),
			interceptors.UnaryServerLogging(logger),
		),
	)
	loanv1.RegisterLoanCommandServiceServer(grpcSrv, grpcserver.NewLoanServer(loanSvc, adjSvc, disbSvc, colSvc))
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)

	go func() {
		logger.Info("http server started", "name", cfg.AppName, "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	go func() {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			logger.Error("grpc listen", "err", err)
			os.Exit(1)
		}
		logger.Info("grpc server started", "name", cfg.AppName, "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("grpc server error", "err", err)
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
	grpcSrv.GracefulStop()
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
