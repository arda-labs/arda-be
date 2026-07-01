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

	"github.com/arda-labs/arda/apps/media-service/internal/config"
	"github.com/arda-labs/arda/apps/media-service/internal/handler"
	"github.com/arda-labs/arda/apps/media-service/internal/migration"
	"github.com/arda-labs/arda/apps/media-service/internal/repository"
	"github.com/arda-labs/arda/apps/media-service/internal/service"
	"github.com/arda-labs/arda/apps/media-service/internal/storage"
	transport "github.com/arda-labs/arda/apps/media-service/internal/transport/http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	ctx := context.Background()
	db, err := sql.Open("postgres", cfg.DatabaseDSN)
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

	provider, err := storage.NewS3Provider(ctx, storage.S3Config{
		Endpoint:       cfg.StorageEndpoint,
		Region:         cfg.StorageRegion,
		AccessKey:      cfg.StorageAccessKey,
		SecretKey:      cfg.StorageSecretKey,
		ForcePathStyle: cfg.StorageForcePathStyle,
	})
	if err != nil {
		logger.Error("init storage provider", "err", err)
		os.Exit(1)
	}

	repo := repository.NewMediaRepository(db)
	mediaSvc := service.NewMediaService(cfg, repo, provider)
	mediaHandler := handler.NewMediaHandler(mediaSvc)

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
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(mediaHandler),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("http server started", "name", cfg.AppName, "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down", "name", cfg.AppName)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
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
