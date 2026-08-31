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
	FinanceServiceURL   string
	EnableReadTools     bool
	EnableHITLProposals bool
	EnableCodeMode      bool
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

	// ModelGatewayToken is the AI Gateway credential sent as the
	// cf-aig-authorization header when the model base URL points at a
	// Cloudflare AI Gateway with authentication enabled. Empty = direct.
	ModelGatewayToken string

	// ModelBaseURLAllowlist restricts which base URLs tenant settings may
	// point at (gateway routing, §3.5 of docs/ai/agent-evolution-roadmap.md).
	// Empty slice = enforcement disabled; only ValidateEgressURL applies.
	ModelBaseURLAllowlist []string

	// Knowledge vector retrieval (roadmap §4.2). KnowledgeVectorEnabled turns
	// hybrid search on; without a full embedding config the service logs a
	// warning and keeps full-text-only search.
	KnowledgeVectorEnabled bool
	EmbeddingProvider      string // workersai (default) | openai
	EmbeddingModel         string
	EmbeddingAPIToken      string
	EmbeddingAccountID     string
	EmbeddingBaseURL       string
}

const defaultDirectToolSystemPrompt = `Bạn là Olorin, trợ lý của nền tảng Arda. Bạn trả lời ngắn gọn, chính xác ` +
	`dựa trên dữ liệu tenant hiện tại. Chỉ dùng tool khi cần; mọi hành động thay đổi ` +
	`dữ liệu đều phải chờ con người phê duyệt.`

const defaultCodeModeSystemPrompt = `Bạn là Olorin, trợ lý thông minh của nền tảng Arda.
Bạn tương tác với hệ thống thông qua 2 Meta-Tools:
1. search({ query, domain? }): Tìm kiếm các phương thức TypeScript SDK (arda.*) phù hợp với yêu cầu.
2. execute({ code }): Viết và thực thi mã JavaScript (ES6) để gọi SDK arda.* (ví dụ: await arda.crm.getCustomer({ customerId: "..." })), xử lý mảng (map, filter, reduce, sort) và trả về kết quả cuối cùng.

Quy tắc quan trọng:
- Luôn gọi search() trước để biết chính xác tên hàm và tham số SDK; không tự đoán API.
- Viết code JS trong execute() gọn gàng, sử dụng await cho các lời gọi arda.*, và luôn có lệnh return kết quả.
- Có thể dùng console.log() để ghi nhận log kiểm tra.
- Mọi hành động thay đổi/ghi dữ liệu (mutation) đều tự động chuyển thành đề xuất chờ con người phê duyệt trước khi thực thi.`

func Load() Config {
	enableCodeMode := envBoolOr("AI_ENABLE_CODE_MODE", true)
	defaultPrompt := defaultDirectToolSystemPrompt
	if enableCodeMode {
		defaultPrompt = defaultCodeModeSystemPrompt
	}

	return Config{
		AppName:             envOr("APP_NAME", "ai-service"),
		HTTPAddr:            envOr("HTTP_ADDR", "0.0.0.0:8098"),
		Mode:                envOr("AI_MODE", "spike"),
		DatabaseDSN:         os.Getenv("DATABASE_DSN"),
		ServiceAuthSecret:   os.Getenv("ARDA_SERVICE_AUTH_SECRET"),
		CRMServiceURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("CRM_SERVICE_URL")), "/"),
		FinanceServiceURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("FINANCE_SERVICE_URL")), "/"),
		EnableReadTools:     envBoolOr("AI_ENABLE_READ_TOOLS", false),
		EnableHITLProposals: envBoolOr("AI_ENABLE_HITL_PROPOSALS", false),
		EnableCodeMode:      enableCodeMode,
		DBMaxOpenConns:      envIntOr("DB_MAX_OPEN_CONNS", 8),
		DBMaxIdleConns:      envIntOr("DB_MAX_IDLE_CONNS", 4),
		DBConnMaxIdleSec:    envIntOr("DB_CONN_MAX_IDLE_SECONDS", 300),

		ModelEnabled:       envBoolOr("AI_ENABLE_AGENT", false),
		ModelBaseURL:       strings.TrimRight(strings.TrimSpace(envOr("AI_MODEL_BASE_URL", "https://api.openai.com/v1")), "/"),
		ModelAPIKey:        strings.TrimSpace(os.Getenv("AI_MODEL_API_KEY")),
		ModelID:            strings.TrimSpace(os.Getenv("AI_MODEL_ID")),
		ModelSystemPrompt:  envOr("AI_MODEL_SYSTEM_PROMPT", defaultPrompt),
		AgentMaxSteps:      envIntOr("AI_AGENT_MAX_STEPS", 6),
		RateLimitPerMinute: envIntOr("AI_RATE_LIMIT_PER_MINUTE", 30),
		ModelGatewayToken:  strings.TrimSpace(os.Getenv("AI_MODEL_GATEWAY_TOKEN")),

		ModelBaseURLAllowlist: envListOr("AI_MODEL_BASE_URL_ALLOWLIST"),

		KnowledgeVectorEnabled: envBoolOr("AI_KNOWLEDGE_VECTOR", false),
		EmbeddingProvider:      envOr("AI_EMBEDDING_PROVIDER", "workersai"),
		EmbeddingModel:         envOr("AI_EMBEDDING_MODEL", "@cf/qwen/qwen3-embedding-0.6b"),
		EmbeddingAPIToken:      os.Getenv("AI_EMBEDDING_API_TOKEN"),
		EmbeddingAccountID:     os.Getenv("AI_EMBEDDING_ACCOUNT_ID"),
		EmbeddingBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("AI_EMBEDDING_BASE_URL")), "/"),
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

func envListOr(name string) []string {
	raw := os.Getenv(name)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []string
	for _, item := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}
