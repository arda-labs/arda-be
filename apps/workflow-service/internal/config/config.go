package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppName              string `yaml:"app_name"`
	HTTPAddr             string `yaml:"http_addr"`
	GRPCAddr             string `yaml:"grpc_addr"`
	LogLevel             string `yaml:"log_level"`
	DatabaseDSN          string `yaml:"database_dsn"`
	ZeebeAddr            string `yaml:"zeebe_addr"`
	ZeebeRestAddr        string `yaml:"zeebe_rest_addr"`
	ZeebeTasklistAddr    string `yaml:"zeebe_tasklist_addr"`
	ZeebeESURL           string `yaml:"zeebe_es_url"`
	CRMGRPCAddr          string `yaml:"crm_grpc_addr"`
	LoanGRPCAddr         string `yaml:"loan_grpc_addr"`
	FinanceGRPCAddr      string `yaml:"finance_grpc_addr"`
	DepositGRPCAddr      string `yaml:"deposit_grpc_addr"`
	CapitalGRPCAddr      string `yaml:"capital_grpc_addr"`
	HRMGRPCAddr          string `yaml:"hrm_grpc_addr"`
	IAMGRPCAddr          string `yaml:"iam_grpc_addr"`
	NotificationGRPCAddr string `yaml:"notification_grpc_addr"`
	StatisticalGRPCAddr  string `yaml:"statistical_grpc_addr"`
}

func Load() Config {
	cfg := Config{
		AppName:              "workflow-service",
		HTTPAddr:             "0.0.0.0:8093",
		GRPCAddr:             "0.0.0.0:9090",
		LogLevel:             "info",
		DatabaseDSN:          "",
		ZeebeAddr:            "192.168.100.201:30650",
		CRMGRPCAddr:          "localhost:9090",
		LoanGRPCAddr:         "localhost:9090",
		FinanceGRPCAddr:      "localhost:9090",
		DepositGRPCAddr:      "localhost:9090",
		HRMGRPCAddr:          "localhost:9090",
		NotificationGRPCAddr: "localhost:9090",
	}

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/workflow-service/config.yaml"} {
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
	envStr("ZEEBE_ADDR", &cfg.ZeebeAddr)
	envStr("ZEEBE_REST_ADDR", &cfg.ZeebeRestAddr)
	envStr("ZEEBE_TASKLIST_ADDR", &cfg.ZeebeTasklistAddr)
	envStr("ZEEBE_ES_URL", &cfg.ZeebeESURL)
	envStr("CRM_GRPC_ADDR", &cfg.CRMGRPCAddr)
	envStr("LOAN_GRPC_ADDR", &cfg.LoanGRPCAddr)
	envStr("FINANCE_GRPC_ADDR", &cfg.FinanceGRPCAddr)
	envStr("DEPOSIT_GRPC_ADDR", &cfg.DepositGRPCAddr)
	envStr("CAPITAL_GRPC_ADDR", &cfg.CapitalGRPCAddr)
	envStr("HRM_GRPC_ADDR", &cfg.HRMGRPCAddr)
	envStr("IAM_GRPC_ADDR", &cfg.IAMGRPCAddr)
	envStr("NOTIFICATION_GRPC_ADDR", &cfg.NotificationGRPCAddr)
	envStr("STATISTICAL_GRPC_ADDR", &cfg.StatisticalGRPCAddr)

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
	set("grpc_addr", &c.GRPCAddr)
	set("log_level", &c.LogLevel)
	set("database_dsn", &c.DatabaseDSN)
	set("zeebe_addr", &c.ZeebeAddr)
	set("zeebe_rest_addr", &c.ZeebeRestAddr)
	set("zeebe_tasklist_addr", &c.ZeebeTasklistAddr)
	set("zeebe_es_url", &c.ZeebeESURL)
	set("crm_grpc_addr", &c.CRMGRPCAddr)
	set("loan_grpc_addr", &c.LoanGRPCAddr)
	set("iam_grpc_addr", &c.IAMGRPCAddr)
	set("notification_grpc_addr", &c.NotificationGRPCAddr)
	set("statistical_grpc_addr", &c.StatisticalGRPCAddr)
	set("capital_grpc_addr", &c.CapitalGRPCAddr)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}
