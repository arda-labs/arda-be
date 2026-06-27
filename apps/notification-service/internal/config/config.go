package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppName   string `yaml:"app_name"`
	HTTPAddr  string `yaml:"http_addr"`
	LogLevel  string `yaml:"log_level"`
	ZeebeAddr string `yaml:"zeebe_addr"`
}

func Load() Config {
	cfg := Config{
		AppName:   "notification-service",
		HTTPAddr:  "0.0.0.0:8080",
		LogLevel:  "info",
		ZeebeAddr: "192.168.100.201:30650",
	}

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/notification-service/config.yaml"} {
			if cfg.loadYAML(p) {
				break
			}
		}
	}

	envStr("APP_NAME", &cfg.AppName)
	envStr("HTTP_ADDR", &cfg.HTTPAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("ZEEBE_ADDR", &cfg.ZeebeAddr)

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
	set("log_level", &c.LogLevel)
	set("zeebe_addr", &c.ZeebeAddr)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}
