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
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardaredis "github.com/arda-labs/arda/libs/go/arda-redis"
	"github.com/joho/godotenv"
)

func main() {
	// Local dotenv loading is opt-in. Production receives configuration through
	// the process environment/Secret references and must not search parent
	// directories for an implicit fallback file.
	if os.Getenv("ARDA_LOAD_DOTENV") == "true" {
		for _, path := range []string{".env", "../.env", "../../.env"} {
			if err := godotenv.Load(path); err == nil {
				break
			}
		}
	}

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
	iam := iamclient.New(cfg.IAMServiceURL, os.Getenv("ARDA_SERVICE_AUTH_SECRET"))
	if err := iam.ValidateServiceIdentity(); err != nil {
		logger.Error("auth-gateway IAM service identity is not configured", "err", err)
		os.Exit(1)
	}
	_ = iam

	// ── ForwardAuth handler ──
	authHandler := handler.NewAuthHandler(verifier, iam, pol, logger, time.Duration(cfg.IAMContextCacheTTL)*time.Second)

	// ── Session store ──
	var sessStore session.Store
	switch cfg.SessionStore {
	case "redis":
		if cfg.RedisURL == "" {
			logger.Error("redis session store requires REDIS_URL")
			os.Exit(1)
		}
		rdb, err := ardaredis.Connect(context.Background(), cfg.RedisURL)
		if err != nil {
			logger.Error("redis connect", "err", err)
			os.Exit(1)
		}
		sessStore = session.NewRedisStore(rdb)
		logger.Info("session store: redis")
	case "memory":
		sessStore = session.NewMemoryStore()
		logger.Warn("session store: in-memory; explicit development-only configuration")
	default:
		logger.Error("invalid SESSION_STORE; expected redis or memory", "value", cfg.SessionStore)
		os.Exit(1)
	}

	// ── BFF handler ──
	bffHandler := handler.NewBFFHandler(cfg, sessStore, iam, pol)

	// ── HTTP server ──
	// WriteTimeout must be 0 so SSE (/api/notifications/stream) is not cut after 10s.
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, transport.NewRouter(authHandler, bffHandler, cfg)),
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
