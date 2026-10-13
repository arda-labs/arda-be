package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

type Config struct {
	AppName          string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr         string `yaml:"http_addr" env:"HTTP_ADDR"`
	GRPCAddr         string `yaml:"grpc_addr" env:"GRPC_ADDR"`
	LogLevel         string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN      string `yaml:"database_dsn" env:"DATABASE_DSN"`
	WorkflowGRPCAddr string `yaml:"workflow_grpc_addr" env:"WORKFLOW_GRPC_ADDR"`
}

func Load() Config {
	cfg := Config{
		AppName:          "crm-service",
		HTTPAddr:         "0.0.0.0:8094",
		GRPCAddr:         "0.0.0.0:9090",
		LogLevel:         "info",
		DatabaseDSN:      "",
		WorkflowGRPCAddr: "localhost:9090",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("crm-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
