package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

type Config struct {
	AppName           string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr          string `yaml:"http_addr" env:"HTTP_ADDR"`
	GRPCAddr          string `yaml:"grpc_addr" env:"GRPC_ADDR"`
	LogLevel          string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN       string `yaml:"database_dsn" env:"DATABASE_DSN"`
	NATSURL           string `yaml:"nats_url" env:"NATS_URL"`
	SLAMilestones     string `yaml:"sla_milestones" env:"WORKFLOW_SLA_MILESTONES"`
	ZeebeAddr         string `yaml:"zeebe_addr" env:"ZEEBE_ADDR"`
	ZeebeRestAddr     string `yaml:"zeebe_rest_addr" env:"ZEEBE_REST_ADDR"`
	ZeebeTasklistAddr string `yaml:"zeebe_tasklist_addr" env:"ZEEBE_TASKLIST_ADDR"`
	ZeebeESURL        string `yaml:"zeebe_es_url" env:"ZEEBE_ES_URL"`
	CRMGRPCAddr       string `yaml:"crm_grpc_addr" env:"CRM_GRPC_ADDR"`
	LoanGRPCAddr      string `yaml:"loan_grpc_addr" env:"LOAN_GRPC_ADDR"`
	// FinanceGRPCAddr, DepositGRPCAddr and HRMGRPCAddr were never read from
	// the YAML file; keep them env-only.
	FinanceGRPCAddr      string `yaml:"-" env:"FINANCE_GRPC_ADDR"`
	DepositGRPCAddr      string `yaml:"-" env:"DEPOSIT_GRPC_ADDR"`
	CapitalGRPCAddr      string `yaml:"capital_grpc_addr" env:"CAPITAL_GRPC_ADDR"`
	HRMGRPCAddr          string `yaml:"-" env:"HRM_GRPC_ADDR"`
	IAMGRPCAddr          string `yaml:"iam_grpc_addr" env:"IAM_GRPC_ADDR"`
	NotificationGRPCAddr string `yaml:"notification_grpc_addr" env:"NOTIFICATION_GRPC_ADDR"`
	StatisticalGRPCAddr  string `yaml:"statistical_grpc_addr" env:"STATISTICAL_GRPC_ADDR"`
}

func Load() Config {
	cfg := Config{
		AppName:              "workflow-service",
		HTTPAddr:             "0.0.0.0:8093",
		GRPCAddr:             "0.0.0.0:9090",
		LogLevel:             "info",
		DatabaseDSN:          "",
		SLAMilestones:        "50,90,100",
		ZeebeAddr:            "localhost:26500",
		CRMGRPCAddr:          "localhost:9090",
		LoanGRPCAddr:         "localhost:9090",
		FinanceGRPCAddr:      "localhost:9090",
		DepositGRPCAddr:      "localhost:9090",
		HRMGRPCAddr:          "localhost:9090",
		NotificationGRPCAddr: "localhost:9090",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("workflow-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
