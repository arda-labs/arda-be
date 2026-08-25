package config

import (
	"os"
	"strconv"
	"strings"
)

// Config controls the AI service runtime. Spike mode is intentionally the
// default for local protocol tests; production mode requires a database.
type Config struct {
	AppName           string
	HTTPAddr          string
	Mode              string
	DatabaseDSN       string
	ServiceAuthSecret string
	CRMServiceURL     string
	EnableReadTools   bool
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxIdleSec  int
}

func Load() Config {
	return Config{
		AppName:           envOr("APP_NAME", "ai-service"),
		HTTPAddr:          envOr("HTTP_ADDR", "0.0.0.0:8098"),
		Mode:              envOr("AI_MODE", "spike"),
		DatabaseDSN:       os.Getenv("DATABASE_DSN"),
		ServiceAuthSecret: os.Getenv("ARDA_SERVICE_AUTH_SECRET"),
		CRMServiceURL:     strings.TrimRight(strings.TrimSpace(os.Getenv("CRM_SERVICE_URL")), "/"),
		EnableReadTools:   envBoolOr("AI_ENABLE_READ_TOOLS", false),
		DBMaxOpenConns:    envIntOr("DB_MAX_OPEN_CONNS", 8),
		DBMaxIdleConns:    envIntOr("DB_MAX_IDLE_CONNS", 4),
		DBConnMaxIdleSec:  envIntOr("DB_CONN_MAX_IDLE_SECONDS", 300),
	}
}

func envBoolOr(name string, fallback bool) bool {
	value, err := strconv.ParseBool(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envIntOr(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
