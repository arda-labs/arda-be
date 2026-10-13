package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for platform-service.
type Config struct {
	AppName     string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr    string `yaml:"http_addr" env:"HTTP_ADDR"`
	GRPCAddr    string `yaml:"grpc_addr" env:"GRPC_ADDR"`
	LogLevel    string `yaml:"log_level" env:"LOG_LEVEL"`
	DatabaseDSN string `yaml:"database_dsn" env:"DATABASE_DSN"`
	NATSURL     string `yaml:"nats_url" env:"NATS_URL"`

	// JobTargetURLs maps a job's owner_service to its base URL, from
	// JOB_TARGET_URLS in the form "svc=url,svc2=url2". It is optional: unset
	// leaves an empty map so the scheduler starts and reports every target as
	// unconfigured.
	JobTargetURLs          map[string]string `yaml:"-" env:"JOB_TARGET_URLS"`
	JobNotifyDefaultTenant string            `yaml:"job_notify_default_tenant" env:"JOB_NOTIFY_DEFAULT_TENANT"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:       "platform-service",
		HTTPAddr:      "0.0.0.0:8091",
		GRPCAddr:      "0.0.0.0:9091",
		LogLevel:      "info",
		DatabaseDSN:   "",
		NATSURL:       "",
		JobTargetURLs: map[string]string{},
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("platform-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
