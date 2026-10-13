package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for loan-service.
type Config struct {
	AppName          string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr         string `yaml:"http_addr" env:"HTTP_ADDR"`
	GRPCAddr         string `yaml:"grpc_addr" env:"GRPC_ADDR"`
	LogLevel         string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN      string `yaml:"database_dsn" env:"DATABASE_DSN"`
	NATSURL          string `yaml:"nats_url" env:"NATS_URL"`
	WorkflowGRPCAddr string `yaml:"workflow_grpc_addr" env:"WORKFLOW_GRPC_ADDR"`
	FinanceGRPCAddr  string `yaml:"finance_grpc_addr" env:"FINANCE_GRPC_ADDR"`
	PlatformGRPCAddr string `yaml:"platform_grpc_addr" env:"PLATFORM_GRPC_ADDR"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:          "loan-service",
		HTTPAddr:         "0.0.0.0:8097",
		GRPCAddr:         "0.0.0.0:9090",
		LogLevel:         "info",
		DatabaseDSN:      "",
		WorkflowGRPCAddr: "",
		FinanceGRPCAddr:  "localhost:9090",
		PlatformGRPCAddr: "platform-service:9090",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("loan-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
