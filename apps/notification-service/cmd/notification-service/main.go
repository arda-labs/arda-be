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

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"

	"github.com/arda-labs/arda/apps/notification-service/internal/config"
	appevents "github.com/arda-labs/arda/apps/notification-service/internal/events"
	"github.com/arda-labs/arda/apps/notification-service/internal/handler"
	"github.com/arda-labs/arda/apps/notification-service/internal/migration"
	"github.com/arda-labs/arda/apps/notification-service/internal/push"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
	"github.com/arda-labs/arda/apps/notification-service/internal/service"
	notificationgrpc "github.com/arda-labs/arda/apps/notification-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/notification-service/internal/transport/http"
	"github.com/arda-labs/arda/apps/notification-service/internal/worker"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	notificationv1 "github.com/arda-labs/arda/libs/go/arda-proto/notification/v1"
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

	db, err := sql.Open("postgres", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("Failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	ardapostgres.ConfigureDefaultPool(db, logger)

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.PingContext(dbCtx); err != nil {
		dbCancel()
		logger.Error("Failed to connect to database", "err", err)
		os.Exit(1)
	}
	dbCancel()

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("Failed to run migrations", "err", err)
		os.Exit(1)
	}

	notificationRepo := repository.NewNotificationRepository(db)
	pushSender := push.NewSender(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
	if pushSender != nil {
		logger.Info("web push enabled")
	} else {
		logger.Warn("web push disabled — set VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY")
	}
	notificationService := service.NewNotificationService(notificationRepo, pushSender)
	notificationHandler := handler.NewNotificationHandler(notificationService)
	serviceSecret, err := identity.SecretFromEnv()
	if err != nil {
		logger.Error("service identity unavailable", "err", err)
		os.Exit(1)
	}
	serverCreds, err := identity.ServerTransportCredentials()
	if err != nil {
		logger.Error("grpc tls unavailable", "err", err)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer(
		grpc.Creds(serverCreds),
		grpc.ChainUnaryInterceptor(interceptors.UnaryServerServiceAuth(serviceSecret, "notification-service", map[string]struct{}{"workflow-service": {}})),
	)
	notificationv1.RegisterNotificationServiceServer(grpcSrv, notificationgrpc.NewServer(notificationService))
	grpc_health_v1.RegisterHealthServer(grpcSrv, health.NewServer())
	go func() {
		lis, listenErr := net.Listen("tcp", cfg.GRPCAddr)
		if listenErr != nil {
			logger.Error("grpc listen", "err", listenErr)
			os.Exit(1)
		}
		logger.Info("gRPC server started", "addr", cfg.GRPCAddr)
		if serveErr := grpcSrv.Serve(lis); serveErr != nil {
			logger.Error("grpc server error", "err", serveErr)
			os.Exit(1)
		}
	}()

	deliveryWorker := worker.NewDeliveryWorker(notificationRepo)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	go deliveryWorker.Run(workerCtx)
	defer stopWorker()

	if cfg.NATSURL == "" {
		logger.Error("notification outbox requires NATS_URL")
		os.Exit(1)
	}
	nc, err := nats.Connect(cfg.NATSURL, nats.Name(cfg.AppName))
	if err != nil {
		logger.Error("NATS connection required for notification outbox", "err", err)
		os.Exit(1)
	}
	defer nc.Close()
	publisher, publisherErr := appevents.NewNATSPublisher(nc)
	if publisherErr != nil {
		logger.Error("JetStream required for notification outbox", "err", publisherErr)
		os.Exit(1)
	}
	outboxWorker := worker.NewOutboxWorker(notificationRepo, publisher)
	go outboxWorker.Run(workerCtx)
	logger.Info("Notification JetStream outbox publisher started", "nats_url", cfg.NATSURL)

	// Keep SSE streams open (inbox poll). Read header timeout only.
	srv := &http.Server{
		Addr:        cfg.HTTPAddr,
		Handler:     ardahttp.MetricsMiddleware(cfg.AppName, transport.NewRouter(notificationHandler)),
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second,
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
	stopWorker()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown error", "err", err)
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
