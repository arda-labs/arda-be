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

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"

	"github.com/arda-labs/arda/apps/notification-service/internal/config"
	appevents "github.com/arda-labs/arda/apps/notification-service/internal/events"
	"github.com/arda-labs/arda/apps/notification-service/internal/handler"
	"github.com/arda-labs/arda/apps/notification-service/internal/migration"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
	"github.com/arda-labs/arda/apps/notification-service/internal/service"
	transport "github.com/arda-labs/arda/apps/notification-service/internal/transport/http"
	"github.com/arda-labs/arda/apps/notification-service/internal/worker"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
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
	notificationService := service.NewNotificationService(notificationRepo)
	notificationHandler := handler.NewNotificationHandler(notificationService)

	deliveryWorker := worker.NewDeliveryWorker(notificationRepo)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	go deliveryWorker.Run(workerCtx)
	defer stopWorker()

	if cfg.NATSURL != "" {
		nc, err := nats.Connect(cfg.NATSURL, nats.Name(cfg.AppName))
		if err != nil {
			logger.Warn("NATS unavailable; notification outbox will remain pending", "err", err)
		} else {
			defer nc.Close()
			outboxWorker := worker.NewOutboxWorker(notificationRepo, appevents.NewNATSPublisher(nc))
			go outboxWorker.Run(workerCtx)
			logger.Info("Notification outbox publisher started", "nats_url", cfg.NATSURL)
		}
	}

	// Keep SSE streams open (inbox poll). Read header timeout only.
	srv := &http.Server{
		Addr:        cfg.HTTPAddr,
		Handler:     transport.NewRouter(notificationHandler),
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
