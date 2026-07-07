package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppName     string `yaml:"app_name"`
	HTTPAddr    string `yaml:"http_addr"`
	GRPCAddr    string `yaml:"grpc_addr"`
	LogLevel    string `yaml:"log_level"`
	DatabaseDSN string `yaml:"database_dsn"`
	ZeebeAddr         string `yaml:"zeebe_addr"`
	ZeebeRestAddr     string `yaml:"zeebe_rest_addr"`
	ZeebeTasklistAddr string `yaml:"zeebe_tasklist_addr"`
	ZeebeESURL        string `yaml:"zeebe_es_url"`
	CRMGRPCAddr       string `yaml:"crm_grpc_addr"`
	IAMGRPCAddr       string `yaml:"iam_grpc_addr"`
}

func Load() Config {
	cfg := Config{
		AppName:     "workflow-service",
		HTTPAddr:    "0.0.0.0:8093",
		GRPCAddr:    "0.0.0.0:9093",
		LogLevel:    "info",
		DatabaseDSN: "postgres://postgres:postgres@localhost:5432/workflow?sslmode=disable",
		ZeebeAddr:   "192.168.100.201:30650",
		CRMGRPCAddr: "localhost:9094",
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
	envStr("IAM_GRPC_ADDR", &cfg.IAMGRPCAddr)

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
	set("iam_grpc_addr", &c.IAMGRPCAddr)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}
