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

	"github.com/arda-labs/arda/apps/platform-service/internal/config"
	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/handler"
	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	grpcserver "github.com/arda-labs/arda/apps/platform-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/platform-service/internal/transport/http"
	grpciam "github.com/arda-labs/arda/libs/go/arda-grpc/client/iam"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardamedia "github.com/arda-labs/arda/libs/go/arda-media"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

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

	repo := repository.NewPlatformRepository(db)
	calendarRepo := repository.NewCalendarRepository(db)
	menuRepo := repository.NewMenuRepository(db)

	platformSvc := service.NewPlatformService(repo)
	calendarSvc := service.NewCalendarService(calendarRepo)
	menuSvc := service.NewMenuService(menuRepo)

	mediaClient, err := ardamedia.NewClient("platform-service")
	if err != nil {
		logger.Error("media grpc client is required; refusing to start", "err", err)
		os.Exit(1)
	}
	defer mediaClient.Close()
	platformHandler := handler.NewPlatformHandler(platformSvc, mediaClient)
	menuHandler := handler.NewMenuHandler(menuSvc)
	iamAddr := os.Getenv("IAM_GRPC_ADDR")
	if iamAddr == "" {
		iamAddr = "iam-service:9090"
	}
	iamClient, err := grpciam.Dial(context.Background(), iamAddr, "platform-service")
	if err != nil {
		logger.Error("iam grpc client is required for EOD tenant discovery", "err", err)
		os.Exit(1)
	}
	defer iamClient.Close()
	eodSvc := service.NewEODService(db, logger, calendarSvc, iamClient, cfg.JobTargetURLs)
	calendarHandler := handler.NewCalendarHandler(calendarSvc, eodSvc)
	eodHandler := handler.NewEODHandler(eodSvc)

	// Scheduled jobs: appCtx drives the scheduler loop and the outbox relay;
	// it is cancelled first on shutdown.
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	serviceSecret, secretErr := identity.SecretFromEnv()
	if secretErr != nil {
		logger.Warn("scheduled jobs disabled: service identity secret missing", "err", secretErr)
	}
	jobRepo := repository.NewScheduledJobRepository(db)
	if err := jobRepo.SeedCatalog(appCtx, domain.JobCatalog, cfg.JobNotifyDefaultTenant, time.Now()); err != nil {
		logger.Error("seed scheduled jobs", "err", err)
		os.Exit(1)
	}
	jobExecutor := service.NewJobExecutor(cfg.JobTargetURLs, serviceSecret, &http.Client{})
	jobScheduler := service.NewJobScheduler(jobRepo, jobExecutor, logger)
	scheduledJobSvc := service.NewScheduledJobService(jobRepo, jobScheduler, jobExecutor, cfg.JobNotifyDefaultTenant)
	scheduledJobHandler := handler.NewScheduledJobHandler(scheduledJobSvc)
	internalJobsHandler := handler.NewInternalJobsHandler(eodSvc, jobRepo)
	if cfg.NATSURL == "" {
		logger.Warn("platform outbox relay disabled: NATS_URL is empty")
	} else if nc, err := nats.Connect(cfg.NATSURL, nats.Name(cfg.AppName), nats.Timeout(5*time.Second)); err != nil {
		logger.Warn("platform outbox relay disabled: nats unavailable", "err", err)
	} else {
		defer nc.Close()
		if relay := service.NewJobOutboxRelay(db, nc, logger); relay != nil {
			go relay.Run(appCtx)
		}
	}

	router := transport.NewRouter(platformHandler, calendarHandler, menuHandler, eodHandler,
		transport.WithScheduledJobs(scheduledJobHandler),
		transport.WithInternalJobs(internalJobsHandler),
	)
	srv := &http.Server{
		Addr:        cfg.HTTPAddr,
		Handler:     ardahttp.HandlerChain(cfg.AppName, nil, ardahttp.UserTimezoneMiddleware(router), jobScheduler.RenderMetrics),
		ReadTimeout: 10 * time.Second,
		// Must stay >= the longest platform-owned job timeout: the scheduler
		// calls POST /internal/jobs/eod on this server with a 330s timeout.
		WriteTimeout: 6 * time.Minute,
		IdleTimeout:  60 * time.Second,
		// 16KiB is generous for a JSON API. net/http defaults to 1MiB of headers
		// per connection, which is a cheap way to occupy a worker.
		MaxHeaderBytes: 16 << 10,
	}

	if secretErr != nil {
		logger.Error("service identity is not configured", "err", secretErr)
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
			interceptors.UnaryServerRecovery(logger),
			interceptors.UnaryServerServiceAuthMethodSources(
				serviceSecret, "platform-service",
				map[string]struct{}{"finance-service": {}},
				map[string]map[string]struct{}{
					"/arda.platform.v1.PlatformService/ResolveParameter": {"finance-service": {}, "loan-service": {}},
					"/arda.platform.v1.PlatformService/ListLookupValues": {"loan-service": {}},
					"/arda.platform.v1.PlatformService/GetBusinessDate":  {"loan-service": {}},
					"/arda.platform.v1.PlatformService/IsWorkingDay":     {"loan-service": {}},
				},
			),
			interceptors.UnaryServerLogging(logger),
		),
	)
	platformv1.RegisterPlatformServiceServer(grpcSrv, grpcserver.NewPlatformServer(platformSvc, calendarSvc))
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)

	// Bind synchronously: the scheduler (started below) self-calls this server
	// for platform-owned jobs on its first tick, so the listener must be up.
	httpLis, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("http listen", "err", err)
		os.Exit(1)
	}
	go func() {
		logger.Info("http server started", "name", cfg.AppName, "addr", cfg.HTTPAddr)
		if err := srv.Serve(httpLis); err != nil && err != http.ErrServerClosed {
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

	// Start the scheduler only after the HTTP listener is bound: platform-owned
	// jobs (eod, platform-runs-cleanup) are dispatched to this same HTTP server,
	// and Run ticks immediately. A missing secret already exited above (gRPC
	// needs it), so the guard is defensive.
	if secretErr == nil {
		go jobScheduler.Run(appCtx)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down", "name", cfg.AppName)
	// Shutdown order:
	//  1. appCancel: stop the scheduler loop and the outbox relay.
	//  2. Drain the scheduler while HTTP is still serving: no new dispatches
	//     (its closing flag also guards the WaitGroup Add/Wait race), and
	//     in-flight runs — including self-calls to /internal/jobs/eod and
	//     /internal/jobs/platform-runs-cleanup — can finish instead of failing
	//     and raising false failure notifications. A manual run requested
	//     during the drain gets ErrSchedulerBusy (409). Runs still going at the
	//     deadline are reaped later by a peer.
	//  3. HTTP shutdown, then 4. gRPC.
	appCancel()
	jobCtx, jobCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer jobCancel()
	if err := jobScheduler.Shutdown(jobCtx); err != nil {
		logger.Warn("scheduled job runs still in flight at shutdown deadline", "err", err)
	}
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
