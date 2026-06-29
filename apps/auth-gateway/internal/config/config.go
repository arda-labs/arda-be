package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration.
type Config struct {
	AppName       string `yaml:"app_name"`
	HTTPAddr      string `yaml:"http_addr"`
	LogLevel      string `yaml:"log_level"`
	TokenStrategy string `yaml:"token_strategy"`

	JWTSecret                 string `yaml:"jwt_secret"`
	JWTIssuer                 string `yaml:"jwt_issuer"`
	JWTAudience               string `yaml:"jwt_audience"`
	IntrospectionURL          string `yaml:"introspection_url"`
	IntrospectionClientID     string `yaml:"introspection_client_id"`
	IntrospectionClientSecret string `yaml:"introspection_client_secret"`

	IAMServiceURL      string `yaml:"iam_service_url"`
	PlatformServiceURL string `yaml:"platform_service_url"`
	ProxyBackendURL    string `yaml:"proxy_backend_url"`
	PolicyFile         string `yaml:"policy_file"`

	RedisURL            string `yaml:"redis_url"`
	SessionCookieName   string `yaml:"session_cookie_name"`
	SessionCookieDomain string `yaml:"session_cookie_domain"`
	SessionTTL          int    `yaml:"session_ttl_seconds"`
	CookieSecure        bool   `yaml:"cookie_secure"`
	CookieSameSite      string `yaml:"cookie_same_site"`

	// Kratos + Hydra
	KratosPublicURL  string `yaml:"kratos_public_url"`
	HydraAdminURL    string `yaml:"hydra_admin_url"`
	HydraPublicURL   string `yaml:"hydra_public_url"`
	OAuthClientID    string `yaml:"oauth_client_id"`
	OAuthRedirectURI string `yaml:"oauth_redirect_uri"`
}

func (c Config) ProxyURL() string {
	if c.ProxyBackendURL != "" {
		return c.ProxyBackendURL
	}
	return c.IAMServiceURL
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:       "auth-gateway",
		HTTPAddr:      "0.0.0.0:8082",
		LogLevel:      "info",
		TokenStrategy: "jwt",
		JWTSecret:     "super-secret-dev-key-change-in-production",
		JWTIssuer:     "https://auth.arda.io.vn",
		JWTAudience:   "arda-api",
		IAMServiceURL:      "http://localhost:8081",
		PlatformServiceURL: "http://localhost:8091",
		PolicyFile:         "configs/policy.yaml",
		SessionCookieName:   "arda_sid",
		SessionTTL:     86400,
		CookieSecure:   true,
		CookieSameSite: "Lax",
		HydraPublicURL: "https://auth.arda.io.vn",
		OAuthClientID:  "arda-shell",
		OAuthRedirectURI: "http://localhost:5000/callback",
	}

	// Try loading YAML
	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/auth-gateway/config.yaml"} {
			if cfg.loadYAML(p) {
				break
			}
		}
	}

	// Env overrides
	envStr("APP_NAME", &cfg.AppName)
	envStr("HTTP_ADDR", &cfg.HTTPAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("TOKEN_STRATEGY", &cfg.TokenStrategy)
	envStr("JWT_SECRET", &cfg.JWTSecret)
	envStr("JWT_ISSUER", &cfg.JWTIssuer)
	envStr("JWT_AUDIENCE", &cfg.JWTAudience)
	envStr("INTROSPECTION_URL", &cfg.IntrospectionURL)
	envStr("INTROSPECTION_CLIENT_ID", &cfg.IntrospectionClientID)
	envStr("INTROSPECTION_CLIENT_SECRET", &cfg.IntrospectionClientSecret)
	envStr("IAM_SERVICE_URL", &cfg.IAMServiceURL)
	envStr("PLATFORM_SERVICE_URL", &cfg.PlatformServiceURL)
	envStr("PROXY_BACKEND_URL", &cfg.ProxyBackendURL)
	envStr("POLICY_FILE", &cfg.PolicyFile)
	envStr("REDIS_URL", &cfg.RedisURL)
	envStr("SESSION_COOKIE_NAME", &cfg.SessionCookieName)
	envStr("SESSION_COOKIE_DOMAIN", &cfg.SessionCookieDomain)
	envInt("SESSION_TTL_SECONDS", &cfg.SessionTTL)
	envBool("COOKIE_SECURE", &cfg.CookieSecure)
	envStr("COOKIE_SAMESITE", &cfg.CookieSameSite)
	envStr("KRATOS_PUBLIC_URL", &cfg.KratosPublicURL)
	envStr("HYDRA_ADMIN_URL", &cfg.HydraAdminURL)
	envStr("HYDRA_PUBLIC_URL", &cfg.HydraPublicURL)
	envStr("OAUTH_CLIENT_ID", &cfg.OAuthClientID)
	envStr("OAUTH_REDIRECT_URI", &cfg.OAuthRedirectURI)

	return cfg
}

func (c *Config) loadYAML(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	m := make(map[string]interface{})
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
	setStr("token_strategy", &c.TokenStrategy)
	setStr("jwt_secret", &c.JWTSecret)
	setStr("jwt_issuer", &c.JWTIssuer)
	setStr("jwt_audience", &c.JWTAudience)
	setStr("introspection_url", &c.IntrospectionURL)
	setStr("introspection_client_id", &c.IntrospectionClientID)
	setStr("introspection_client_secret", &c.IntrospectionClientSecret)
	setStr("iam_service_url", &c.IAMServiceURL)
	setStr("platform_service_url", &c.PlatformServiceURL)
	setStr("proxy_backend_url", &c.ProxyBackendURL)
	setStr("policy_file", &c.PolicyFile)
	setStr("redis_url", &c.RedisURL)
	setStr("session_cookie_name", &c.SessionCookieName)
	setStr("session_cookie_domain", &c.SessionCookieDomain)
	if v, ok := m["session_ttl_seconds"].(int); ok { c.SessionTTL = v }
	if v, ok := m["cookie_secure"].(bool); ok { c.CookieSecure = v }
	setStr("cookie_same_site", &c.CookieSameSite)
	setStr("kratos_public_url", &c.KratosPublicURL)
	setStr("hydra_admin_url", &c.HydraAdminURL)
	setStr("hydra_public_url", &c.HydraPublicURL)
	setStr("oauth_client_id", &c.OAuthClientID)
	setStr("oauth_redirect_uri", &c.OAuthRedirectURI)
	return true
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}

func envInt(key string, target *int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*target = n
		}
	}
}

func envBool(key string, target *bool) {
	if v := os.Getenv(key); v != "" {
		*target = v == "true" || v == "1"
	}
}
