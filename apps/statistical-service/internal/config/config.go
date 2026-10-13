package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for statistical-service.
type Config struct {
	AppName          string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr         string `yaml:"http_addr" env:"HTTP_ADDR"`
	LogLevel         string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN      string `yaml:"database_dsn" env:"DATABASE_DSN"`
	WorkflowGRPCAddr string `yaml:"workflow_grpc_addr" env:"WORKFLOW_GRPC_ADDR"`
	// Reporting ETL sources (signed /internal/reporting/*). Empty disables
	// that source (reported as skipped, not failed).
	LoanServiceURL    string `yaml:"loan_service_url" env:"LOAN_SERVICE_URL"`
	DepositServiceURL string `yaml:"deposit_service_url" env:"DEPOSIT_SERVICE_URL"`
	CapitalServiceURL string `yaml:"capital_service_url" env:"CAPITAL_SERVICE_URL"`
	CRMServiceURL     string `yaml:"crm_service_url" env:"CRM_SERVICE_URL"`
	FinanceServiceURL string `yaml:"finance_service_url" env:"FINANCE_SERVICE_URL"`
	NATSURL           string `yaml:"nats_url" env:"NATS_URL"`
	// GotenbergURL enables PDF rendering of report documents (empty disables
	// it: xlsx/html still render).
	GotenbergURL string `yaml:"gotenberg_url" env:"GOTENBERG_URL"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:          "statistical-service",
		HTTPAddr:         "0.0.0.0:8080",
		LogLevel:         "info",
		DatabaseDSN:      "",
		WorkflowGRPCAddr: "localhost:9090",
		NATSURL:          "nats://localhost:4222",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("statistical-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
