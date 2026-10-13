package config

import (
	"fmt"
	"os"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration.
type Config struct {
	AppName       string `yaml:"app_name" env:"APP_NAME"`
	HTTPAddr      string `yaml:"http_addr" env:"HTTP_ADDR"`
	LogLevel      string `yaml:"log_level" env:"LOG_LEVEL"`
	TokenStrategy string `yaml:"token_strategy" env:"TOKEN_STRATEGY"`

	JWTSecret                 string `yaml:"jwt_secret" env:"JWT_SECRET"`
	JWTIssuer                 string `yaml:"jwt_issuer" env:"JWT_ISSUER"`
	JWTAudience               string `yaml:"jwt_audience" env:"JWT_AUDIENCE"`
	JWKSURL                   string `yaml:"jwks_url" env:"JWKS_URL"`
	IntrospectionURL          string `yaml:"introspection_url" env:"INTROSPECTION_URL"`
	IntrospectionClientID     string `yaml:"introspection_client_id" env:"INTROSPECTION_CLIENT_ID"`
	IntrospectionClientSecret string `yaml:"introspection_client_secret" env:"INTROSPECTION_CLIENT_SECRET"`

	IAMServiceURL             string `yaml:"iam_service_url" env:"IAM_SERVICE_URL"`
	IAMContextCacheTTL        int    `yaml:"iam_context_cache_ttl_seconds" env:"IAM_CONTEXT_CACHE_TTL_SECONDS"`
	SlowRequestLogEnabled     bool   `yaml:"slow_request_log_enabled" env:"SLOW_REQUEST_LOG_ENABLED"`
	SlowRequestLogThresholdMS int    `yaml:"slow_request_log_threshold_ms" env:"SLOW_REQUEST_LOG_THRESHOLD_MS"`
	CORSAllowedOrigins        string `yaml:"cors_allowed_origins" env:"CORS_ALLOWED_ORIGINS"`
	FrontendOrigin            string `yaml:"frontend_origin" env:"FRONTEND_ORIGIN"`
	PlatformServiceURL        string `yaml:"platform_service_url" env:"PLATFORM_SERVICE_URL"`
	FinanceServiceURL         string `yaml:"finance_service_url" env:"FINANCE_SERVICE_URL"`
	MediaServiceURL           string `yaml:"media_service_url" env:"MEDIA_SERVICE_URL"`
	WorkflowServiceURL        string `yaml:"workflow_service_url" env:"WORKFLOW_SERVICE_URL"`
	CRMServiceURL             string `yaml:"crm_service_url" env:"CRM_SERVICE_URL"`
	HRMServiceURL             string `yaml:"hrm_service_url" env:"HRM_SERVICE_URL"`
	NotificationURL           string `yaml:"notification_service_url" env:"NOTIFICATION_SERVICE_URL"`
	MDMServiceURL             string `yaml:"mdm_service_url" env:"MDM_SERVICE_URL"`
	AIServiceURL              string `yaml:"ai_service_url" env:"AI_SERVICE_URL"`
	RAGServiceURL             string `yaml:"rag_service_url" env:"RAG_SERVICE_URL"`
	// The loan, deposit, capital and statistical URLs were never read from
	// the YAML file; keep them env-only.
	LoanServiceURL        string `yaml:"-" env:"LOAN_SERVICE_URL"`
	DepositServiceURL     string `yaml:"-" env:"DEPOSIT_SERVICE_URL"`
	CapitalServiceURL     string `yaml:"-" env:"CAPITAL_SERVICE_URL"`
	StatisticalServiceURL string `yaml:"-" env:"STATISTICAL_SERVICE_URL"`
	ServiceAuthSecret     string `yaml:"-" env:"ARDA_SERVICE_AUTH_SECRET"`
	PolicyFile            string `yaml:"policy_file" env:"POLICY_FILE"`

	RedisURL                 string `yaml:"redis_url" env:"REDIS_URL"`
	SessionStore             string `yaml:"session_store" env:"SESSION_STORE"`
	SessionCookieName        string `yaml:"session_cookie_name" env:"SESSION_COOKIE_NAME"`
	SessionCookieDomain      string `yaml:"session_cookie_domain" env:"SESSION_COOKIE_DOMAIN"`
	SessionTTL               int    `yaml:"session_ttl_seconds" env:"SESSION_TTL_SECONDS"`
	SessionAuthCheckInterval int    `yaml:"session_auth_check_interval_seconds" env:"SESSION_AUTH_CHECK_INTERVAL_SECONDS"`
	RecentAuthWindow         int    `yaml:"recent_auth_window_seconds" env:"RECENT_AUTH_WINDOW_SECONDS"`
	CookieSecure             bool   `yaml:"cookie_secure" env:"COOKIE_SECURE"`
	CookieSameSite           string `yaml:"cookie_same_site" env:"COOKIE_SAMESITE"`

	// Kratos + Hydra
	KratosPublicURL   string `yaml:"kratos_public_url" env:"KRATOS_PUBLIC_URL"`
	HydraAdminURL     string `yaml:"hydra_admin_url" env:"HYDRA_ADMIN_URL"`
	HydraPublicURL    string `yaml:"hydra_public_url" env:"HYDRA_PUBLIC_URL"`
	OAuthClientID     string `yaml:"oauth_client_id" env:"OAUTH_CLIENT_ID"`
	OAuthRedirectURI  string `yaml:"oauth_redirect_uri" env:"OAUTH_REDIRECT_URI"`
	OAuthRedirectURIs string `yaml:"oauth_redirect_uris" env:"OAUTH_REDIRECT_URIS"`
}

// Load reads config from YAML file (optional) + env overrides.
func Load() Config {
	cfg := Config{
		AppName:                   "auth-gateway",
		HTTPAddr:                  "0.0.0.0:8082",
		LogLevel:                  "info",
		TokenStrategy:             "jwt",
		JWTSecret:                 "",
		JWTIssuer:                 "https://auth.arda.io.vn",
		JWTAudience:               "arda-api",
		IAMServiceURL:             "http://localhost:8081",
		IAMContextCacheTTL:        60,
		SlowRequestLogEnabled:     false,
		SlowRequestLogThresholdMS: 1000,
		CORSAllowedOrigins:        "https://arda.io.vn,http://localhost:5000",
		FrontendOrigin:            "http://localhost:5000",
		PlatformServiceURL:        "http://localhost:8091",
		FinanceServiceURL:         "http://localhost:8090",
		MediaServiceURL:           "http://localhost:8092",
		WorkflowServiceURL:        "http://localhost:8093",
		CRMServiceURL:             "http://localhost:8094",
		HRMServiceURL:             "http://localhost:8099",
		NotificationURL:           "http://localhost:8095",
		MDMServiceURL:             "http://localhost:8096",
		AIServiceURL:              "http://localhost:8098",
		RAGServiceURL:             "http://localhost:8098",
		PolicyFile:                "configs/policy.yaml",
		SessionCookieName:         "arda_sid",
		SessionStore:              "redis",
		SessionTTL:                86400,
		SessionAuthCheckInterval:  60,
		RecentAuthWindow:          300,
		CookieSecure:              true,
		CookieSameSite:            "Lax",
		HydraPublicURL:            "https://auth.arda.io.vn",
		OAuthClientID:             "arda-shell",
		OAuthRedirectURI:          "http://localhost:5000/api/auth/callback",
		OAuthRedirectURIs:         "https://api.arda.io.vn/api/auth/callback,https://arda.io.vn/callback,http://localhost:5000/api/auth/callback,http://localhost:5000/callback",
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("auth-gateway")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
