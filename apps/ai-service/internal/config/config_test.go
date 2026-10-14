package config

import (
	"strings"
	"testing"
)

func TestLoadEmbeddingConfigIsIndependentAndProductionFailsClosed(t *testing.T) {
	t.Setenv("AI_MODE", "production")
	t.Setenv("AI_MODEL_BASE_URL", "https://chat.example/v1")
	t.Setenv("AI_MODEL_API_KEY", "chat-key")
	t.Setenv("AI_RAG_EMBEDDING_BASE_URL", "https://embed.example/v1")
	t.Setenv("AI_RAG_EMBEDDING_API_KEY", "embed-key")
	t.Setenv("AI_RAG_EMBEDDING_MODEL", "embed-model")
	cfg := Load()
	if !cfg.RAGRequireEmbedding {
		t.Fatal("production mode must require embeddings")
	}
	if cfg.RAGEmbeddingBaseURL != "https://embed.example/v1" || cfg.RAGEmbeddingAPIKey != "embed-key" || cfg.RAGEmbeddingModel != "embed-model" {
		t.Fatalf("independent embedding config not loaded: %+v", cfg)
	}
}

func TestLoadEmbeddingConfigStaysUnsetForDevelopment(t *testing.T) {
	t.Setenv("AI_MODE", "development")
	t.Setenv("AI_MODEL_BASE_URL", "https://chat.example/v1")
	t.Setenv("AI_MODEL_API_KEY", "chat-key")
	t.Setenv("AI_RAG_EMBEDDING_BASE_URL", "")
	t.Setenv("AI_RAG_EMBEDDING_API_KEY", "")
	t.Setenv("AI_RAG_EMBEDDING_MODEL", "")
	cfg := Load()
	if cfg.RAGRequireEmbedding {
		t.Fatal("development mode should allow FTS-only by default")
	}
	if cfg.RAGEmbeddingBaseURL != "" || cfg.RAGEmbeddingAPIKey != "" {
		t.Fatalf("embedding config must not inherit chat config: %+v", cfg)
	}
}

func TestLoadServiceURLsUsesCanonicalRegistry(t *testing.T) {
	for _, envName := range ServiceURLEnv {
		t.Setenv(envName, "")
	}
	t.Setenv("CRM_SERVICE_URL", "http://crm-service:8080/")
	t.Setenv("WORKFLOW_SERVICE_URL", " http://workflow-service:8080 ")

	cfg := Load()
	if got := cfg.ServiceURLs["crm-service"]; got != "http://crm-service:8080" {
		t.Fatalf("crm-service URL must be trimmed of whitespace and trailing slash, got %q", got)
	}
	if got := cfg.ServiceURLs["workflow-service"]; got != "http://workflow-service:8080" {
		t.Fatalf("workflow-service URL not loaded, got %q", got)
	}
	if _, present := cfg.ServiceURLs["mdm-service"]; present {
		t.Fatal("unset service URL must be omitted, not defaulted")
	}
}

func TestCodeModeSystemPromptCarriesAnswerFormMatrix(t *testing.T) {
	t.Setenv("AI_ENABLE_READ_TOOLS", "true")
	t.Setenv("AI_MODEL_SYSTEM_PROMPT", "")
	prompt := Load().ModelSystemPrompt

	for _, want := range []string{
		"Chọn dạng trả lời:",
		"văn xuôi",
		"bảng markdown",
		"renderChart",
		"getReportPresentation",
		"một dạng chính",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("code mode prompt missing %q", want)
		}
	}
	if strings.Count(prompt, "BẮT BUỘC hiển thị biểu đồ") != 0 {
		t.Fatal("duplicate renderChart mandate should have been merged into the answer-form matrix")
	}
	if !strings.Contains(prompt, "không viết HTML") {
		t.Fatal("HTML ban for report mode must stay")
	}
}

func TestDirectToolSystemPromptUnchangedByAnswerFormMatrix(t *testing.T) {
	t.Setenv("AI_ENABLE_READ_TOOLS", "false")
	t.Setenv("AI_MODEL_SYSTEM_PROMPT", "")
	if strings.Contains(Load().ModelSystemPrompt, "Chọn dạng trả lời:") {
		t.Fatal("direct-tool prompt must not carry the answer-form matrix")
	}
}

func TestCodeModePromptMentionsWidgetOnlyWhenFlagOn(t *testing.T) {
	t.Setenv("AI_ENABLE_READ_TOOLS", "true")
	t.Setenv("AI_MODEL_SYSTEM_PROMPT", "")

	t.Setenv("AI_ENABLE_SANDBOXED_UI", "false")
	off := Load().ModelSystemPrompt
	for _, banned := range []string{"renderWidget", "window.ARDA_DATA", "arda:data"} {
		if strings.Contains(off, banned) {
			t.Fatalf("flag off: prompt must not mention %q anywhere", banned)
		}
	}

	t.Setenv("AI_ENABLE_SANDBOXED_UI", "true")
	on := Load().ModelSystemPrompt
	for _, want := range []string{
		"5. renderWidget(",
		"gọi renderWidget",
		"window.ARDA_DATA",
		"Không dùng renderWidget",
		"--chart-1",
		"arda.chart(",
		"arda.fmt(",
	} {
		if !strings.Contains(on, want) {
			t.Fatalf("flag on: prompt missing %q", want)
		}
	}
	if strings.Index(on, "Chọn dạng trả lời:") > strings.Index(on, "gọi renderWidget") {
		t.Fatal("the renderWidget rule must live inside the answer-form matrix")
	}
	if !strings.Contains(on, "không viết HTML") {
		t.Fatal("HTML ban for catalogued reports must stay")
	}
}

func TestLoadSandboxedUIFlagDefaultsOff(t *testing.T) {
	t.Setenv("AI_ENABLE_SANDBOXED_UI", "")
	if Load().EnableSandboxedUI {
		t.Fatal("AI_ENABLE_SANDBOXED_UI must default to false")
	}
	t.Setenv("AI_ENABLE_SANDBOXED_UI", "true")
	if !Load().EnableSandboxedUI {
		t.Fatal("AI_ENABLE_SANDBOXED_UI=true must enable the flag")
	}
}
