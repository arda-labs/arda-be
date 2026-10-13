package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for finance-service.
type Config struct {
	AppName  string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr string `yaml:"http_addr" env:"HTTP_ADDR"`
	GRPCAddr string `yaml:"grpc_addr" env:"GRPC_ADDR"`
	LogLevel string `yaml:"log_level" env:"LOG_LEVEL"`

	DatabaseDSN      string `yaml:"database_dsn" env:"DATABASE_DSN"`
	PlatformGRPCAddr string `yaml:"platform_grpc_addr" env:"PLATFORM_GRPC_ADDR"`
	WorkflowGRPCAddr string `yaml:"workflow_grpc_addr" env:"WORKFLOW_GRPC_ADDR"`
	LoanGRPCAddr     string `yaml:"loan_grpc_addr" env:"LOAN_GRPC_ADDR"`
	NATSURL          string `yaml:"nats_url" env:"NATS_URL"`

	// MaxConcurrentTransactions was never read from the YAML file; keep it
	// env-only.
	MaxConcurrentTransactions int `yaml:"-" env:"MAX_CONCURRENT_TXNS"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:  "finance-service",
		HTTPAddr: "0.0.0.0:8090",
		GRPCAddr: "0.0.0.0:9090",
		LogLevel: "info",

		DatabaseDSN:      "",
		PlatformGRPCAddr: "localhost:9090",
		WorkflowGRPCAddr: "localhost:9090",
		LoanGRPCAddr:     "localhost:9090",
		NATSURL:          "nats://localhost:4222",

		MaxConcurrentTransactions: 100,
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("finance-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
