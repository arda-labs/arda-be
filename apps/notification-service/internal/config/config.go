package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

type Config struct {
	AppName         string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr        string `yaml:"http_addr" env:"HTTP_ADDR"`
	GRPCAddr        string `yaml:"grpc_addr" env:"GRPC_ADDR"`
	LogLevel        string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN     string `yaml:"database_dsn" env:"DATABASE_DSN"`
	NATSURL         string `yaml:"nats_url" env:"NATS_URL,NOTIFICATION_NATS_URL"`
	IAMGRPCAddr     string `yaml:"iam_grpc_addr" env:"IAM_GRPC_ADDR"`
	HRMGRPCAddr     string `yaml:"hrm_grpc_addr" env:"HRM_GRPC_ADDR"`
	DefaultLocale   string `yaml:"default_locale" env:"NOTIFICATION_DEFAULT_LOCALE"`
	ManagementGroup string `yaml:"notification_management_group" env:"NOTIFICATION_MANAGEMENT_GROUP"`
	VAPIDPublicKey  string `yaml:"vapid_public_key" env:"VAPID_PUBLIC_KEY"`
	VAPIDPrivateKey string `yaml:"vapid_private_key" env:"VAPID_PRIVATE_KEY"`
	VAPIDSubject    string `yaml:"vapid_subject" env:"VAPID_SUBJECT"`
}

func Load() Config {
	cfg := Config{
		AppName:       "notification-service",
		HTTPAddr:      "0.0.0.0:8095",
		GRPCAddr:      "0.0.0.0:9090",
		LogLevel:      "info",
		DatabaseDSN:   "",
		NATSURL:       "",
		VAPIDSubject:  "mailto:ops@arda.io.vn",
		DefaultLocale: "vi-VN",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("notification-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
