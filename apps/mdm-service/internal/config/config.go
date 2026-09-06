package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for mdm-service.
type Config struct {
	AppName     string `yaml:"app_name"`
	HTTPAddr    string `yaml:"http_addr"`
	LogLevel    string `yaml:"log_level"`
	DatabaseDSN string `yaml:"database_dsn"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:     "mdm-service",
		HTTPAddr:    "0.0.0.0:8096",
		LogLevel:    "info",
		DatabaseDSN: "",
	}

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/mdm-service/config.yaml"} {
			if cfg.loadYAML(p) {
				break
			}
		}
	}

	envStr("APP_NAME", &cfg.AppName)
	envStr("HTTP_ADDR", &cfg.HTTPAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("DATABASE_DSN", &cfg.DatabaseDSN)

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
	setStr := func(key string, target *string) {
		if v, ok := m[key].(string); ok {
			*target = v
		}
	}
	setStr("app_name", &c.AppName)
	setStr("http_addr", &c.HTTPAddr)
	setStr("log_level", &c.LogLevel)
	setStr("database_dsn", &c.DatabaseDSN)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}
