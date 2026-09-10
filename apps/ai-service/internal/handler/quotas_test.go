package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeQuotaStore struct {
	settings *repository.QuotaSettings
}

func (s *fakeQuotaStore) Start(context.Context, repository.RunContext, string) error { return nil }

func (s *fakeQuotaStore) Finish(context.Context, repository.RunContext, string, string) error {
	return nil
}

func (s *fakeQuotaStore) GetQuotaSettings(_ context.Context, tenantID string) (*repository.QuotaSettings, error) {
	if s.settings != nil {
		return s.settings, nil
	}
	return &repository.QuotaSettings{
		TenantID:   tenantID,
		WebhookURL: "https://hooks.slack.com/services/T00/B00/XXXX",
	}, nil
}

func (s *fakeQuotaStore) SaveQuotaSettings(_ context.Context, settings repository.QuotaSettings) error {
	s.settings = &settings
	return nil
}

func TestQuotaHandlers(t *testing.T) {
	store := &fakeQuotaStore{}
	router := NewRouter(store)

	// GET without auth -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/quotas", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	// GET with auth -> 200 with default quota settings
	req = httptest.NewRequest(http.MethodGet, "/api/ai/settings/quotas", nil)
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "usr-1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hooks.slack.com") {
		t.Fatalf("expected hooks.slack.com in response: %s", w.Body.String())
	}

	// PUT update quotas
	updateBody := `{"webhookUrl":"https://custom-webhook.com","monthlyTokenLimit":100000}`
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
