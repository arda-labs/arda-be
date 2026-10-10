package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for platform-service.
type Config struct {
	AppName     string `yaml:"app_name"`
	HTTPAddr    string `yaml:"http_addr"`
	GRPCAddr    string `yaml:"grpc_addr"`
	LogLevel    string `yaml:"log_level"`
	DatabaseDSN string `yaml:"database_dsn"`
	NATSURL     string `yaml:"nats_url"`

	JobTargetURLs          map[string]string `yaml:"-"`
	JobNotifyDefaultTenant string            `yaml:"job_notify_default_tenant"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:     "platform-service",
		HTTPAddr:    "0.0.0.0:8091",
		GRPCAddr:    "0.0.0.0:9091",
		LogLevel:    "info",
		DatabaseDSN: "",
		NATSURL:     "",
	}

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/platform-service/config.yaml"} {
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
	envStr("NATS_URL", &cfg.NATSURL)
	envStr("JOB_NOTIFY_DEFAULT_TENANT", &cfg.JobNotifyDefaultTenant)
	cfg.JobTargetURLs = ParseTargets(os.Getenv("JOB_TARGET_URLS"))

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
	setStr("grpc_addr", &c.GRPCAddr)
	setStr("log_level", &c.LogLevel)
	setStr("database_dsn", &c.DatabaseDSN)
	setStr("nats_url", &c.NATSURL)
	setStr("job_notify_default_tenant", &c.JobNotifyDefaultTenant)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}

// ParseTargets reads "svc=url,svc2=url2". Malformed pairs are ignored.
func ParseTargets(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if ok && k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}
