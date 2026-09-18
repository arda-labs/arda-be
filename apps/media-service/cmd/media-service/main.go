package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/media-service/internal/config"
	"github.com/arda-labs/arda/apps/media-service/internal/events"
	"github.com/arda-labs/arda/apps/media-service/internal/handler"
	"github.com/arda-labs/arda/apps/media-service/internal/migration"
	"github.com/arda-labs/arda/apps/media-service/internal/repository"
	"github.com/arda-labs/arda/apps/media-service/internal/service"
	"github.com/arda-labs/arda/apps/media-service/internal/storage"
	grpcserver "github.com/arda-labs/arda/apps/media-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/media-service/internal/transport/http"
	"github.com/arda-labs/arda/apps/media-service/internal/worker"
	ardadoc "github.com/arda-labs/arda/libs/go/arda-doc"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	mediav1 "github.com/arda-labs/arda/libs/go/arda-proto/media/v1"
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

	ctx := context.Background()
	db, err := sql.Open("pgx/v5", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	ardapostgres.ConfigureDefaultPool(db, logger)

	if err := db.PingContext(ctx); err != nil {
		logger.Error("ping database", "err", err)
		os.Exit(1)
	}

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("run migrations", "err", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	provider, err := storage.NewS3Provider(ctx, storageConfigFrom(cfg))
	if err != nil {
		logger.Error("init storage provider", "err", err)
		os.Exit(1)
	}

	repo := repository.NewMediaRepository(db)
	serviceOpts := make([]service.Option, 0, 1)
	if gotenbergURL := strings.TrimSpace(cfg.GotenbergURL); gotenbergURL != "" {
		converter, err := ardadoc.NewGotenbergClient(gotenbergURL)
		if err != nil {
			logger.Error("init gotenberg client", "err", err)
			os.Exit(1)
		}
		serviceOpts = append(serviceOpts, service.WithDocumentConverter(converter))
		logger.Info("office preview conversion enabled", "gotenberg_url", gotenbergURL)
	} else {
		logger.Warn("office preview conversion disabled: GOTENBERG_URL is not configured")
	}
	mediaSvc := service.NewMediaService(cfg, repo, provider, serviceOpts...)
	mediaHandler := handler.NewMediaHandler(mediaSvc)

	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()

	// Publish the media outbox to NATS JetStream. Uploads keep working when
	// NATS is down: rows stay pending and are retried on the next start.
	if cfg.NATSURL == "" {
		logger.Warn("media outbox relay disabled: NATS_URL is not configured; events stay pending")
	} else if nc, err := nats.Connect(cfg.NATSURL, nats.Name(cfg.AppName), nats.Timeout(5*time.Second)); err != nil {
		logger.Error("media outbox relay disabled: NATS connection failed", "err", err)
	} else {
		defer nc.Close()
		publisher, publisherErr := events.NewNATSPublisher(nc)
		if publisherErr != nil {
			logger.Error("media outbox relay disabled: JetStream unavailable", "err", publisherErr)
		} else {
			go worker.NewOutboxWorker(repo, publisher).Run(workerCtx)
			logger.Info("media outbox relay started", "nats_url", cfg.NATSURL)
		}
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
			interceptors.UnaryServerRecovery(logger),
			interceptors.UnaryServerServiceAuth(serviceSecret, "media-service", map[string]struct{}{
				"iam-service":      {},
				"platform-service": {},
			}),
			interceptors.UnaryServerLogging(logger),
		),
	)
	mediav1.RegisterMediaServiceServer(grpcSrv, grpcserver.NewMediaServer(mediaSvc))
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)

	// Start background worker for cleaning expired temporary uploads
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		ctx := context.Background()
		// Run initial check
		if count, err := mediaSvc.CleanupExpiredTempFiles(ctx); err != nil {
			logger.Error("failed to cleanup expired temp files", "err", err)
		} else if count > 0 {
			logger.Info("cleaned up expired temp files on startup", "count", count)
		}

		for range ticker.C {
			if count, err := mediaSvc.CleanupExpiredTempFiles(ctx); err != nil {
				logger.Error("failed to cleanup expired temp files", "err", err)
			} else if count > 0 {
				logger.Info("cleaned up expired temp files", "count", count)
			}
		}
	}()

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: ardahttp.MetricsMiddleware(cfg.AppName, transport.NewRouter(mediaHandler)),
		// Headers are small and fast; bodies are bounded by MaxBytesReader
		// (upload_max_size_mb), so only the header read is time-boxed.
		ReadHeaderTimeout: 10 * time.Second,
		// Preview conversion runs synchronously through Gotenberg
		// (--api-timeout=120s) and downloads stream to slow clients, so the
		// write deadline must cover a full conversion plus transfer.
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

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
	stopWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
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

// storageConfigFrom maps service config onto the storage client. Kept separate
// so the public endpoint wiring stays covered by a unit test.
func storageConfigFrom(cfg config.Config) storage.S3Config {
	return storage.S3Config{
		Endpoint:       cfg.StorageEndpoint,
		PublicEndpoint: cfg.StoragePublicEndpoint,
		Region:         cfg.StorageRegion,
		AccessKey:      cfg.StorageAccessKey,
		SecretKey:      cfg.StorageSecretKey,
		ForcePathStyle: cfg.StorageForcePathStyle,
	}
}
