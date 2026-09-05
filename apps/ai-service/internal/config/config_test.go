package config

import "testing"

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
