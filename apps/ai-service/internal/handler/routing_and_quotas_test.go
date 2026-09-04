package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeRoutingAndQuotaStore struct {
	fakeSettingsStore
	rules    *repository.TenantRoutingRules
	budgets  []repository.DepartmentBudget
	settings *repository.QuotaSettings
}

func (s *fakeRoutingAndQuotaStore) GetRoutingRules(_ context.Context, tenantID string) (*repository.TenantRoutingRules, error) {
	if s.rules != nil {
		return s.rules, nil
	}
	return &repository.TenantRoutingRules{
		TenantID:          tenantID,
		FastModel:         "gemini-2.5-flash",
		CodeModel:         "claude-3.5-sonnet",
		SensitiveModel:    "qwen2.5:7b-instruct-q4_K_M",
		PrimaryProvider:   "gemini",
		SecondaryProvider: "openai",
		FailoverProvider:  "ollama",
	}, nil
}

func (s *fakeRoutingAndQuotaStore) SaveRoutingRules(_ context.Context, rules repository.TenantRoutingRules) (*repository.TenantRoutingRules, error) {
	s.rules = &rules
	return &rules, nil
}

func (s *fakeRoutingAndQuotaStore) ListDepartmentBudgets(_ context.Context, tenantID string) ([]repository.DepartmentBudget, error) {
	if s.budgets != nil {
		return s.budgets, nil
	}
	return []repository.DepartmentBudget{
		{TenantID: tenantID, Department: "Tech & DevOps", MonthlyLimit: 300, Spent: 118.2, RPMLimit: 120},
	}, nil
}

func (s *fakeRoutingAndQuotaStore) SaveDepartmentBudgets(_ context.Context, _ string, budgets []repository.DepartmentBudget) error {
	s.budgets = budgets
	return nil
}

func (s *fakeRoutingAndQuotaStore) GetQuotaSettings(_ context.Context, tenantID string) (*repository.QuotaSettings, error) {
	if s.settings != nil {
		return s.settings, nil
	}
	return &repository.QuotaSettings{
		TenantID:   tenantID,
		WebhookURL: "https://hooks.slack.com/services/T00/B00/XXXX",
	}, nil
}

func (s *fakeRoutingAndQuotaStore) SaveQuotaSettings(_ context.Context, settings repository.QuotaSettings) error {
	s.settings = &settings
	return nil
}

func TestRoutingHandlers(t *testing.T) {
	store := &fakeRoutingAndQuotaStore{}
	router := NewRouter(store)

	// GET without auth -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/routing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	// GET with auth -> 200 with default rules
	req = httptest.NewRequest(http.MethodGet, "/api/ai/settings/routing", nil)
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "gemini-2.5-flash") {
		t.Fatalf("expected gemini-2.5-flash in response: %s", w.Body.String())
	}

	// PUT update routing rules
	updateBody := `{"fastModel":"gpt-4o-mini","codeModel":"claude-3.5-sonnet","sensitiveModel":"local-llama","primaryProvider":"openai","secondaryProvider":"anthropic","failoverProvider":"ollama"}`
	req = httptest.NewRequest(http.MethodPut, "/api/ai/settings/routing", strings.NewReader(updateBody))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "gpt-4o-mini") {
		t.Fatalf("expected gpt-4o-mini in updated response: %s", w.Body.String())
	}
}

func TestQuotaHandlers(t *testing.T) {
	store := &fakeRoutingAndQuotaStore{}
	router := NewRouter(store)

	// GET with auth
	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/quotas", nil)
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Tech") {
		t.Fatalf("expected Tech in response: %s", w.Body.String())
	}

	// PUT update quotas
	updateBody := `{"budgets":[{"department":"Tech & DevOps","monthlyLimit":500,"spent":150,"rpmLimit":200}],"webhookUrl":"https://custom-webhook.com"}`
	req = httptest.NewRequest(http.MethodPut, "/api/ai/settings/quotas", strings.NewReader(updateBody))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "custom-webhook.com") {
		t.Fatalf("expected custom-webhook.com in response: %s", w.Body.String())
	}
}
