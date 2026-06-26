package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for iam-service.
type Config struct {
	AppName  string `yaml:"app_name"`
	HTTPAddr string `yaml:"http_addr"`
	LogLevel string `yaml:"log_level"`

	DatabaseDSN string `yaml:"database_dsn"`

	HydraAdminURL     string `yaml:"hydra_admin_url"`
	HydraPublicURL    string `yaml:"hydra_public_url"`
	HydraIssuerURL    string `yaml:"hydra_issuer_url"`
	HydraClientID     string `yaml:"hydra_client_id"`
	HydraClientSecret string `yaml:"hydra_client_secret"`
	HydraRedirectURI  string `yaml:"hydra_redirect_uri"`

	KratosAdminURL string `yaml:"kratos_admin_url"`

	RedisURL    string `yaml:"redis_url"`
	TOTPIssuer  string `yaml:"totp_issuer"`
	AuditEnabled bool `yaml:"audit_enabled"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:  "iam-service",
		HTTPAddr: "0.0.0.0:8080",
		LogLevel: "info",

		DatabaseDSN: "postgres://postgres:postgres@localhost:5432/iam?sslmode=disable",

		HydraAdminURL:     "http://hydra-admin:4445",
		HydraPublicURL:    "https://auth.arda.io.vn",
		HydraIssuerURL:    "https://auth.arda.io.vn",
		HydraClientID:     "arda-shell",
		HydraClientSecret: "",
		HydraRedirectURI:  "http://localhost:5000/login",

		KratosAdminURL: "http://localhost:4434",

		TOTPIssuer:   "arda.io.vn",
		AuditEnabled: true,
	}

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/iam-service/config.yaml"} {
			if cfg.loadYAML(p) {
				break
			}
		}
	}

	envStr("APP_NAME", &cfg.AppName)
	envStr("HTTP_ADDR", &cfg.HTTPAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("DATABASE_DSN", &cfg.DatabaseDSN)
	envStr("HYDRA_ADMIN_URL", &cfg.HydraAdminURL)
	envStr("HYDRA_PUBLIC_URL", &cfg.HydraPublicURL)
	envStr("HYDRA_ISSUER_URL", &cfg.HydraIssuerURL)
	envStr("HYDRA_CLIENT_ID", &cfg.HydraClientID)
	envStr("HYDRA_CLIENT_SECRET", &cfg.HydraClientSecret)
	envStr("HYDRA_REDIRECT_URI", &cfg.HydraRedirectURI)
	envStr("KRATOS_ADMIN_URL", &cfg.KratosAdminURL)
	envStr("REDIS_URL", &cfg.RedisURL)
	envStr("TOTP_ISSUER", &cfg.TOTPIssuer)
	envBool("AUDIT_ENABLED", &cfg.AuditEnabled)

	return cfg
}

func (c *Config) loadYAML(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	m := make(map[string]any)
	if err := yaml.Unmarshal(data, &m); err != nil {
		fmt.Fprintf(os.Stderr, "config: parse %s: %v\n", path, err)
		return false
	}
	set := func(key string, target *string) {
		if v, ok := m[key].(string); ok { *target = v }
	}
	set("app_name", &c.AppName)
	set("http_addr", &c.HTTPAddr)
	set("log_level", &c.LogLevel)
	set("database_dsn", &c.DatabaseDSN)
	set("hydra_admin_url", &c.HydraAdminURL)
	set("hydra_public_url", &c.HydraPublicURL)
	set("hydra_issuer_url", &c.HydraIssuerURL)
	set("hydra_client_id", &c.HydraClientID)
	set("hydra_client_secret", &c.HydraClientSecret)
	set("hydra_redirect_uri", &c.HydraRedirectURI)
	set("kratos_admin_url", &c.KratosAdminURL)
	set("redis_url", &c.RedisURL)
	set("totp_issuer", &c.TOTPIssuer)
	if v, ok := m["audit_enabled"].(bool); ok { c.AuditEnabled = v }
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" { *target = v }
}
func envBool(key string, target *bool) {
	if v := os.Getenv(key); v != "" { *target = v == "true" || v == "1" }
}
var _ = strconv.Itoa
