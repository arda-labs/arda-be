package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeGuardrailsAndStrategiesStore struct {
	fakeSettingsStore
	guardrails *repository.TenantGuardrails
	strategy   *repository.TenantRAGStrategy
}

func (s *fakeGuardrailsAndStrategiesStore) GetGuardrails(_ context.Context, tenantID string) (*repository.TenantGuardrails, error) {
	if s.guardrails != nil {
		return s.guardrails, nil
	}
	return &repository.TenantGuardrails{
		TenantID:               tenantID,
		PromptInjectionDefense: true,
		PIIMasking:             true,
		HallucinationCheck:     true,
		ZeroRetention:          true,
		InjectionThreshold:     0.85,
	}, nil
}

func (s *fakeGuardrailsAndStrategiesStore) SaveGuardrails(_ context.Context, g repository.TenantGuardrails) (*repository.TenantGuardrails, error) {
	s.guardrails = &g
	return &g, nil
}

func (s *fakeGuardrailsAndStrategiesStore) GetRAGStrategy(_ context.Context, tenantID string) (*repository.TenantRAGStrategy, error) {
	if s.strategy != nil {
		return s.strategy, nil
	}
	return &repository.TenantRAGStrategy{
		TenantID:            tenantID,
		Strategy:            "hierarchical",
		ParentChunkSize:     1024,
		ChildChunkSize:      256,
		SimilarityThreshold: 0.82,
		RerankerModel:       "cohere-rerank-v3.5",
		TopK:                20,
		TopN:                5,
	}, nil
}

func (s *fakeGuardrailsAndStrategiesStore) SaveRAGStrategy(_ context.Context, st repository.TenantRAGStrategy) (*repository.TenantRAGStrategy, error) {
	s.strategy = &st
	return &st, nil
}

func TestGuardrailsHandlers(t *testing.T) {
	store := &fakeGuardrailsAndStrategiesStore{}
	router := NewRouter(store)

	// GET without auth -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/guardrails", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	// GET with auth -> 200
	req = httptest.NewRequest(http.MethodGet, "/api/ai/settings/guardrails", nil)
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "injectionThreshold") {
		t.Fatalf("expected injectionThreshold in response: %s", w.Body.String())
	}

	// PUT update guardrails
	body := `{"promptInjectionDefense":false,"piiMasking":true,"hallucinationCheck":false,"zeroRetention":true,"injectionThreshold":0.9}`
	req = httptest.NewRequest(http.MethodPut, "/api/ai/settings/guardrails", strings.NewReader(body))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"promptInjectionDefense":false`) {
		t.Fatalf("expected updated promptInjectionDefense in response: %s", w.Body.String())
	}
}

func TestStrategiesHandlers(t *testing.T) {
	store := &fakeGuardrailsAndStrategiesStore{}
	router := NewRouter(store)

	// GET with auth
	req := httptest.NewRequest(http.MethodGet, "/api/rag/strategies", nil)
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hierarchical") {
		t.Fatalf("expected hierarchical in response: %s", w.Body.String())
	}

	// PUT update strategy
	body := `{"strategy":"semantic","parentChunkSize":2048,"childChunkSize":512,"similarityThreshold":0.88,"rerankerModel":"bge-reranker-large","topK":30,"topN":10}`
	req = httptest.NewRequest(http.MethodPut, "/api/rag/strategies", strings.NewReader(body))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "bge-reranker-large") {
		t.Fatalf("expected bge-reranker-large in response: %s", w.Body.String())
	}
}
