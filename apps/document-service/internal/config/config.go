package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/document-service/internal/extractor"
	"gopkg.in/yaml.v3"
)

const (
	LocationLocal = "local"
	LocationCloud = "cloud"
)

type Config struct {
	AppName  string
	HTTPAddr string
	LogLevel string

	ModelPreset  string
	ModelBaseURL string
	ModelName    string
	ModelAPIKey  string
	// RequestTimeout bounds one extract request end to end, including the
	// single retry. auth-gateway gives upstreams 30s, so keep it below that.
	RequestTimeout time.Duration
	MaxImageSide   int
	MaxImageBytes  int64

	// RequireLocal refuses to start when the model is outside LocalHosts.
	// Production sets it (personal data must not leave the cluster).
	RequireLocal bool
	LocalHosts   []string
}

type rawConfig struct {
	AppName  string `yaml:"app_name"`
	HTTPAddr string `yaml:"http_addr"`
	LogLevel string `yaml:"log_level"`
	Model    struct {
		Preset       string `yaml:"preset"`
		BaseURL      string `yaml:"base_url"`
		Name         string `yaml:"name"`
		Timeout      string `yaml:"timeout"`
		MaxImageSide int    `yaml:"max_image_side"`
	} `yaml:"model"`
	Privacy struct {
		RequireLocal *bool    `yaml:"require_local"`
		LocalHosts   []string `yaml:"local_hosts"`
	} `yaml:"privacy"`
	Limits struct {
		MaxImageBytes int64 `yaml:"max_image_bytes"`
	} `yaml:"limits"`
}

func Load() Config {
	cfg := Config{
		AppName:        "document-service",
		HTTPAddr:       "0.0.0.0:8100",
		LogLevel:       "info",
		ModelPreset:    extractor.PresetOpenAICompatible,
		RequestTimeout: 25 * time.Second,
		MaxImageSide:   1600,
		MaxImageBytes:  10 << 20,
		LocalHosts:     []string{"*.svc.cluster.local", "localhost", "127.0.0.1", "192.168.10.0/24"},
	}
	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/document-service/config.yaml"} {
			if cfg.loadYAML(p) {
				break
			}
		}
	}
	envStr("APP_NAME", &cfg.AppName)
	envStr("HTTP_ADDR", &cfg.HTTPAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("DOCUMENT_MODEL_PRESET", &cfg.ModelPreset)
	envStr("DOCUMENT_MODEL_BASE_URL", &cfg.ModelBaseURL)
	envStr("DOCUMENT_MODEL_NAME", &cfg.ModelName)
	envStr("DOCUMENT_MODEL_API_KEY", &cfg.ModelAPIKey)
	envDuration("DOCUMENT_MODEL_TIMEOUT", &cfg.RequestTimeout)
	envInt("DOCUMENT_MAX_IMAGE_SIDE", &cfg.MaxImageSide)
	envInt64("DOCUMENT_MAX_IMAGE_BYTES", &cfg.MaxImageBytes)
	envBool("DOCUMENT_REQUIRE_LOCAL", &cfg.RequireLocal)
	envList("DOCUMENT_LOCAL_HOSTS", &cfg.LocalHosts)
	return cfg
}

// ModelLocation is "local" when the model host matches LocalHosts.
func (c Config) ModelLocation() string {
	u, err := url.Parse(strings.TrimSpace(c.ModelBaseURL))
	if err != nil || u.Hostname() == "" {
		return LocationCloud
	}
	if HostIsLocal(u.Hostname(), c.LocalHosts) {
		return LocationLocal
	}
	return LocationCloud
}

// ModelConfigured reports whether extraction can run. A cloud model needs a key.
func (c Config) ModelConfigured() bool {
	if strings.TrimSpace(c.ModelBaseURL) == "" || strings.TrimSpace(c.ModelName) == "" {
		return false
	}
	return c.ModelAPIKey != "" || c.ModelLocation() == LocationLocal
}

func (c Config) Validate() error {
	switch c.ModelPreset {
	case extractor.PresetOpenAICompatible, extractor.PresetOpenCodeGo:
	default:
		return fmt.Errorf("unknown model preset %q", c.ModelPreset)
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("model timeout must be positive")
	}
	if c.MaxImageSide < 256 {
		return fmt.Errorf("max image side %d is below 256", c.MaxImageSide)
	}
	if c.MaxImageBytes <= 0 {
		return fmt.Errorf("max image bytes must be positive")
	}
	if c.RequireLocal && strings.TrimSpace(c.ModelBaseURL) != "" && c.ModelLocation() != LocationLocal {
		return fmt.Errorf("DOCUMENT_REQUIRE_LOCAL is set but model base URL %q is not a local host", c.ModelBaseURL)
	}
	return nil
}

// HostIsLocal matches host against exact names, "*.suffix" wildcards and CIDRs.
func HostIsLocal(host string, patterns []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	ip := net.ParseIP(host)
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		switch {
		case p == "":
		case strings.Contains(p, "/"):
			if _, n, err := net.ParseCIDR(p); err == nil && ip != nil && n.Contains(ip) {
				return true
			}
		case strings.HasPrefix(p, "*."):
			if strings.HasSuffix(host, p[1:]) {
				return true
			}
		case host == p:
			return true
		}
	}
	return false
}

func (c *Config) loadYAML(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false
	}
	setStr(raw.AppName, &c.AppName)
	setStr(raw.HTTPAddr, &c.HTTPAddr)
	setStr(raw.LogLevel, &c.LogLevel)
	setStr(raw.Model.Preset, &c.ModelPreset)
	setStr(raw.Model.BaseURL, &c.ModelBaseURL)
	setStr(raw.Model.Name, &c.ModelName)
	if d, err := time.ParseDuration(raw.Model.Timeout); err == nil && d > 0 {
		c.RequestTimeout = d
	}
	if raw.Model.MaxImageSide > 0 {
		c.MaxImageSide = raw.Model.MaxImageSide
	}
	if raw.Privacy.RequireLocal != nil {
		c.RequireLocal = *raw.Privacy.RequireLocal
	}
	if len(raw.Privacy.LocalHosts) > 0 {
		c.LocalHosts = raw.Privacy.LocalHosts
	}
	if raw.Limits.MaxImageBytes > 0 {
		c.MaxImageBytes = raw.Limits.MaxImageBytes
	}
	return true
}

func setStr(v string, dst *string) {
	if strings.TrimSpace(v) != "" {
		*dst = strings.TrimSpace(v)
	}
}

func envStr(key string, dst *string) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		*dst = v
	}
}

func envDuration(key string, dst *time.Duration) {
	if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key))); err == nil && d > 0 {
		*dst = d
	}
}

func envInt(key string, dst *int) {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && n > 0 {
		*dst = n
	}
}

func envInt64(key string, dst *int64) {
	if n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64); err == nil && n > 0 {
		*dst = n
	}
}

func envBool(key string, dst *bool) {
	if b, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key))); err == nil {
		*dst = b
	}
}

func envList(key string, dst *[]string) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		*dst = out
	}
}
