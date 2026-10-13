package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for iam-service.
type Config struct {
	AppName  string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr string `yaml:"http_addr" env:"HTTP_ADDR"`
	// GRPCAddr was never read from the YAML file; keep it env-only.
	GRPCAddr string `yaml:"-" env:"GRPC_ADDR"`
	LogLevel string `yaml:"log_level" env:"LOG_LEVEL"`

	DatabaseDSN string `yaml:"database_dsn" env:"DATABASE_DSN"`

	KratosAdminURL string `yaml:"kratos_admin_url" env:"KRATOS_ADMIN_URL"`
	TOTPIssuer     string `yaml:"totp_issuer" env:"TOTP_ISSUER"`
	HydraAdminURL  string `yaml:"hydra_admin_url" env:"HYDRA_ADMIN_URL"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:  "iam-service",
		HTTPAddr: "0.0.0.0:8080",
		GRPCAddr: "0.0.0.0:9090",
		LogLevel: "info",

		DatabaseDSN: "",

		KratosAdminURL: "http://localhost:4434",
		TOTPIssuer:     "arda.io.vn",
		HydraAdminURL:  "http://localhost:4445",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("iam-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
