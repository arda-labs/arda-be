package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for statistical-service.
type Config struct {
	AppName          string `yaml:"app_name"`
	HTTPAddr         string `yaml:"http_addr"`
	LogLevel         string `yaml:"log_level"`
	DatabaseDSN      string `yaml:"database_dsn"`
	WorkflowGRPCAddr string `yaml:"workflow_grpc_addr"`
	// Reporting ETL sources (signed /internal/reporting/*). Empty disables
	// that source (reported as skipped, not failed).
	LoanServiceURL    string `yaml:"loan_service_url"`
	DepositServiceURL string `yaml:"deposit_service_url"`
	CapitalServiceURL string `yaml:"capital_service_url"`
	CRMServiceURL     string `yaml:"crm_service_url"`
	FinanceServiceURL string `yaml:"finance_service_url"`
	// GotenbergURL enables PDF rendering of report documents (empty disables
	// it: xlsx/html still render).
	GotenbergURL string `yaml:"gotenberg_url"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:          "statistical-service",
		HTTPAddr:         "0.0.0.0:8080",
		LogLevel:         "info",
		DatabaseDSN:      "",
		WorkflowGRPCAddr: "localhost:9090",
	}

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/statistical-service/config.yaml"} {
			if cfg.loadYAML(p) {
				break
			}
		}
	}

	envStr("APP_NAME", &cfg.AppName)
	envStr("HTTP_ADDR", &cfg.HTTPAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("DATABASE_DSN", &cfg.DatabaseDSN)
	envStr("WORKFLOW_GRPC_ADDR", &cfg.WorkflowGRPCAddr)
	envStr("LOAN_SERVICE_URL", &cfg.LoanServiceURL)
	envStr("DEPOSIT_SERVICE_URL", &cfg.DepositServiceURL)
	envStr("CAPITAL_SERVICE_URL", &cfg.CapitalServiceURL)
	envStr("CRM_SERVICE_URL", &cfg.CRMServiceURL)
	envStr("FINANCE_SERVICE_URL", &cfg.FinanceServiceURL)
	envStr("GOTENBERG_URL", &cfg.GotenbergURL)

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
	setStr("workflow_grpc_addr", &c.WorkflowGRPCAddr)
	setStr("loan_service_url", &c.LoanServiceURL)
	setStr("deposit_service_url", &c.DepositServiceURL)
	setStr("capital_service_url", &c.CapitalServiceURL)
	setStr("crm_service_url", &c.CRMServiceURL)
	setStr("finance_service_url", &c.FinanceServiceURL)
	setStr("gotenberg_url", &c.GotenbergURL)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}
