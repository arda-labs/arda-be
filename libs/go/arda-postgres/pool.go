package ardapostgres

import (
	"database/sql"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type PoolOptions struct {
	MaxOpenConns       int
	MaxIdleConns       int
	ConnMaxLifetime    time.Duration
	ConnMaxIdleTime    time.Duration
	MaxOpenConnsEnv    string
	MaxIdleConnsEnv    string
	ConnMaxLifetimeEnv string
	ConnMaxIdleTimeEnv string
}

func DefaultPoolOptions() PoolOptions {
	return PoolOptions{
		MaxOpenConns:       8,
		MaxIdleConns:       4,
		ConnMaxLifetime:    30 * time.Minute,
		ConnMaxIdleTime:    5 * time.Minute,
		MaxOpenConnsEnv:    "DB_MAX_OPEN_CONNS",
		MaxIdleConnsEnv:    "DB_MAX_IDLE_CONNS",
		ConnMaxLifetimeEnv: "DB_CONN_MAX_LIFETIME_SECONDS",
		ConnMaxIdleTimeEnv: "DB_CONN_MAX_IDLE_TIME_SECONDS",
	}
}

func ConfigurePool(db *sql.DB, logger *slog.Logger, opts PoolOptions) {
	if db == nil {
		return
	}
	defaults := DefaultPoolOptions()
	if opts.MaxOpenConnsEnv == "" {
		opts.MaxOpenConnsEnv = defaults.MaxOpenConnsEnv
	}
	if opts.MaxIdleConnsEnv == "" {
		opts.MaxIdleConnsEnv = defaults.MaxIdleConnsEnv
	}
	if opts.ConnMaxLifetimeEnv == "" {
		opts.ConnMaxLifetimeEnv = defaults.ConnMaxLifetimeEnv
	}
	if opts.ConnMaxIdleTimeEnv == "" {
		opts.ConnMaxIdleTimeEnv = defaults.ConnMaxIdleTimeEnv
	}

	maxOpen := envInt(opts.MaxOpenConnsEnv, firstPositive(opts.MaxOpenConns, defaults.MaxOpenConns))
	maxIdle := envInt(opts.MaxIdleConnsEnv, firstPositive(opts.MaxIdleConns, defaults.MaxIdleConns))
	if maxOpen > 0 && maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	lifetime := time.Duration(envInt(opts.ConnMaxLifetimeEnv, int(firstPositiveDuration(opts.ConnMaxLifetime, defaults.ConnMaxLifetime).Seconds()))) * time.Second
	idleTime := time.Duration(envInt(opts.ConnMaxIdleTimeEnv, int(firstPositiveDuration(opts.ConnMaxIdleTime, defaults.ConnMaxIdleTime).Seconds()))) * time.Second

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)
	db.SetConnMaxIdleTime(idleTime)
	if logger != nil {
		logger.Info("database pool configured", "max_open", maxOpen, "max_idle", maxIdle, "max_lifetime", lifetime.String(), "max_idle_time", idleTime.String())
	}
}

func ConfigureDefaultPool(db *sql.DB, logger *slog.Logger) {
	ConfigurePool(db, logger, DefaultPoolOptions())
}

func firstPositive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func firstPositiveDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
