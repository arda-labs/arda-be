package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arda-labs/arda/apps/document-service/internal/cccd"
	"github.com/arda-labs/arda/apps/document-service/internal/config"
	"github.com/arda-labs/arda/apps/document-service/internal/extractor"
	"github.com/arda-labs/arda/apps/document-service/internal/handler"
	"github.com/arda-labs/arda/apps/document-service/internal/metrics"
	transport "github.com/arda-labs/arda/apps/document-service/internal/transport/http"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

var bodyLimits = []ardahttp.BodyLimitOverride{
	{Prefix: "/api/documents/extract", MaxBytes: ardahttp.MaxDocumentExtractBodyBytes},
}

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel(cfg.LogLevel)}))
	slog.SetDefault(logger)

	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	m := metrics.New()
	opts := handler.Options{
		ModelLocation: cfg.ModelLocation(),
		MaxImageBytes: cfg.MaxImageBytes,
		Timeout:       cfg.RequestTimeout,
		Metrics:       m,
		Logger:        logger,
	}
	if cfg.ModelConfigured() {
		client := extractor.NewClient(extractor.ClientConfig{
			Preset:    cfg.ModelPreset,
			BaseURL:   cfg.ModelBaseURL,
			Model:     cfg.ModelName,
			APIKey:    cfg.ModelAPIKey,
			UserAgent: "arda-document-service/1.0",
		}, extractor.NewHTTPClient())
		opts.Extractor = cccd.NewService(client, cfg.ModelName, cfg.ModelLocation(), cfg.MaxImageSide, time.Now)
		logger.Info("document model configured", "model", cfg.ModelName, "model_location", cfg.ModelLocation())
	} else {
		logger.Warn("document model not configured; extraction disabled")
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           ardahttp.HandlerChain(cfg.AppName, bodyLimits, transport.NewRouter(handler.NewDocumentHandler(opts)), m.Render),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 15*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.RequestTimeout+5*time.Second)
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
