package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for capital-service.
type Config struct {
	AppName          string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr         string `yaml:"http_addr" env:"HTTP_ADDR"`
	LogLevel         string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN      string `yaml:"database_dsn" env:"DATABASE_DSN"`
	FinanceGRPCAddr  string `yaml:"finance_grpc_addr" env:"FINANCE_GRPC_ADDR"`
	WorkflowGRPCAddr string `yaml:"workflow_grpc_addr" env:"WORKFLOW_GRPC_ADDR"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:         "capital-service",
		HTTPAddr:        "0.0.0.0:8080",
		LogLevel:        "info",
		DatabaseDSN:     "",
		FinanceGRPCAddr: "localhost:9090",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("capital-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
