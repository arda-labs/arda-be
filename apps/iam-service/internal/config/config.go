package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for iam-service.
type Config struct {
	AppName  string `yaml:"app_name"`
	HTTPAddr string `yaml:"http_addr"`
	GRPCAddr string `yaml:"grpc_addr"`
	LogLevel string `yaml:"log_level"`

	DatabaseDSN string `yaml:"database_dsn"`

	KratosAdminURL string `yaml:"kratos_admin_url"`
	TOTPIssuer     string `yaml:"totp_issuer"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:  "iam-service",
		HTTPAddr: "0.0.0.0:8080",
		GRPCAddr: "0.0.0.0:9090",
		LogLevel: "info",

		DatabaseDSN: "",

		KratosAdminURL: "http://localhost:4434",
		TOTPIssuer:     "arda.io.vn",
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
	envStr("GRPC_ADDR", &cfg.GRPCAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("DATABASE_DSN", &cfg.DatabaseDSN)
	envStr("KRATOS_ADMIN_URL", &cfg.KratosAdminURL)
	envStr("TOTP_ISSUER", &cfg.TOTPIssuer)

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
		if v, ok := m[key].(string); ok {
			*target = v
		}
	}
	set("app_name", &c.AppName)
	set("http_addr", &c.HTTPAddr)
	set("log_level", &c.LogLevel)
	set("database_dsn", &c.DatabaseDSN)
	set("kratos_admin_url", &c.KratosAdminURL)
	set("totp_issuer", &c.TOTPIssuer)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}
func envBool(key string, target *bool) {
	if v := os.Getenv(key); v != "" {
		*target = v == "true" || v == "1"
	}
}
