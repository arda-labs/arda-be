package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/camunda/zeebe/clients/go/v8/pkg/zbc"

	"github.com/arda-labs/arda/apps/notification-service/internal/config"
	transport "github.com/arda-labs/arda/apps/notification-service/internal/transport/http"
	"github.com/arda-labs/arda/apps/notification-service/internal/worker"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	// Zeebe Client & Job Workers
	zeebeClient, err := zbc.NewClient(&zbc.ClientConfig{
		GatewayAddress:         cfg.ZeebeAddr,
		UsePlaintextConnection: true,
	})
	if err != nil {
		logger.Error("Failed to connect to Zeebe gateway", "err", err)
		os.Exit(1)
	}
	defer zeebeClient.Close()
	logger.Info("Connected to Zeebe gateway", "addr", cfg.ZeebeAddr)

	notificationWorkers := worker.NewNotificationWorkers()

	// Start Workers
	worker1 := zeebeClient.NewJobWorker().JobType("notification.email").Handler(notificationWorkers.SendEmailHandler).Open()
	defer worker1.Close()

	worker2 := zeebeClient.NewJobWorker().JobType("notification.sms").Handler(notificationWorkers.SendSMSHandler).Open()
	defer worker2.Close()

	worker3 := zeebeClient.NewJobWorker().JobType("notification.push").Handler(notificationWorkers.SendPushHandler).Open()
	defer worker3.Close()

	logger.Info("Notification job workers registered and listening")

	// Router and HTTP Server
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
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
