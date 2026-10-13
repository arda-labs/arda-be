package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for mdm-service.
type Config struct {
	AppName     string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr    string `yaml:"http_addr" env:"HTTP_ADDR"`
	LogLevel    string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN string `yaml:"database_dsn" env:"DATABASE_DSN"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:     "mdm-service",
		HTTPAddr:    "0.0.0.0:8096",
		LogLevel:    "info",
		DatabaseDSN: "",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("mdm-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
