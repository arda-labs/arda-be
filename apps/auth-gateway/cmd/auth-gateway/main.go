package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/handler"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/policy"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/token"
	transport "github.com/arda-labs/arda/apps/auth-gateway/internal/transport/http"
	ardaredis "github.com/arda-labs/arda/libs/go/arda-redis"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file (optional — không lỗi nếu không có)
	_ = godotenv.Load()          // thư mục hiện tại
	_ = godotenv.Load("../.env") // fallback: thư mục workspace
	_ = godotenv.Load("../../.env")

	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	// ── Route policy ──
	pol, err := policy.Load(cfg.PolicyFile)
	if err != nil {
		logger.Error("load policy", "err", err)
		os.Exit(1)
	}
	logger.Info("policy loaded", "routes", len(pol.Routes))

	// ── Token verifier (for ForwardAuth) ──
	verifier, err := token.New(cfg.TokenStrategy, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTSecret, cfg.JWKSURL, cfg.IntrospectionURL, cfg.IntrospectionClientID, cfg.IntrospectionClientSecret)
	if err != nil {
		logger.Error("create token verifier", "err", err)
		os.Exit(1)
	}
	logger.Info("token verifier ready", "strategy", cfg.TokenStrategy)

	// ── IAM client ──
	iam := iamclient.New(cfg.IAMServiceURL)
	_ = iam

	// ── ForwardAuth handler ──
	authHandler := handler.NewAuthHandler(verifier, iam, pol, logger, time.Duration(cfg.IAMContextCacheTTL)*time.Second)

	// ── Session store ──
	var sessStore session.Store
	if cfg.RedisURL != "" {
		rdb, err := ardaredis.Connect(context.Background(), cfg.RedisURL)
		if err != nil {
			logger.Error("redis connect", "err", err)
			os.Exit(1)
		}
		sessStore = session.NewRedisStore(rdb)
		logger.Info("session store: redis")
	} else {
		sessStore = session.NewMemoryStore()
		logger.Info("session store: in-memory (dev mode)")
	}

	// ── BFF handler ──
	bffHandler := handler.NewBFFHandler(cfg, sessStore, iam, pol)

	// ── HTTP server ──
	// WriteTimeout must be 0 so SSE (/api/notifications/stream) is not cut after 10s.
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(authHandler, bffHandler, cfg),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
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
