package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/handler"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "0.0.0.0:8098"
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler.NewRouter(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("AI protocol spike started", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("AI protocol spike stopped", "err", err)
		os.Exit(1)
	}
}
