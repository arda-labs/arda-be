package config

import (
	"os"
	"strconv"
	"strings"
)

// Config controls the AI service runtime. Spike mode is intentionally the
// default for local protocol tests; production mode requires a database.
type Config struct {
	AppName             string
	HTTPAddr            string
	Mode                string
	DatabaseDSN         string
	ServiceAuthSecret   string
	CRMServiceURL       string
	EnableReadTools     bool
	EnableHITLProposals bool
	DBMaxOpenConns      int
	DBMaxIdleConns      int
	DBConnMaxIdleSec    int

	ModelEnabled       bool
	ModelBaseURL       string
	ModelAPIKey        string
	ModelID            string
	ModelSystemPrompt  string
	AgentMaxSteps      int
	RateLimitPerMinute int
}

const defaultSystemPrompt = `Bạn là Olorin, trợ lý của nền tảng Arda. Bạn trả lời ngắn gọn, chính xác ` +
	`dựa trên dữ liệu tenant hiện tại. Chỉ dùng tool khi cần; mọi hành động thay đổi ` +
	`dữ liệu đều phải chờ con người phê duyệt.`

func Load() Config {
	return Config{
		AppName:             envOr("APP_NAME", "ai-service"),
		HTTPAddr:            envOr("HTTP_ADDR", "0.0.0.0:8098"),
		Mode:                envOr("AI_MODE", "spike"),
		DatabaseDSN:         os.Getenv("DATABASE_DSN"),
		ServiceAuthSecret:   os.Getenv("ARDA_SERVICE_AUTH_SECRET"),
		CRMServiceURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("CRM_SERVICE_URL")), "/"),
		EnableReadTools:     envBoolOr("AI_ENABLE_READ_TOOLS", false),
		EnableHITLProposals: envBoolOr("AI_ENABLE_HITL_PROPOSALS", false),
		DBMaxOpenConns:      envIntOr("DB_MAX_OPEN_CONNS", 8),
		DBMaxIdleConns:      envIntOr("DB_MAX_IDLE_CONNS", 4),
		DBConnMaxIdleSec:    envIntOr("DB_CONN_MAX_IDLE_SECONDS", 300),

		ModelEnabled:       envBoolOr("AI_ENABLE_AGENT", false),
		ModelBaseURL:       strings.TrimRight(strings.TrimSpace(envOr("AI_MODEL_BASE_URL", "https://api.openai.com/v1")), "/"),
		ModelAPIKey:        strings.TrimSpace(os.Getenv("AI_MODEL_API_KEY")),
		ModelID:            strings.TrimSpace(os.Getenv("AI_MODEL_ID")),
		ModelSystemPrompt:  envOr("AI_MODEL_SYSTEM_PROMPT", defaultSystemPrompt),
		AgentMaxSteps:      envIntOr("AI_AGENT_MAX_STEPS", 6),
		RateLimitPerMinute: envIntOr("AI_RATE_LIMIT_PER_MINUTE", 30),
	}
}

func (c Config) ModelReady() bool {
	return c.ModelEnabled && c.ModelBaseURL != "" && c.ModelAPIKey != "" && c.ModelID != ""
}

func envBoolOr(name string, fallback bool) bool {
	value, err := strconv.ParseBool(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envIntOr(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
