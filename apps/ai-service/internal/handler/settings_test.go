package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeSettingsStore struct {
	fakeToolRunStore
	settings map[string]*repository.TenantSettings
}

func (s *fakeSettingsStore) GetTenantSettings(_ context.Context, tenantID string) (*repository.TenantSettings, error) {
	if item, ok := s.settings[tenantID]; ok {
		return item, nil
	}
	return nil, repository.ErrTenantSettingsNotFound
}

func (s *fakeSettingsStore) UpsertTenantSettings(_ context.Context, settings repository.TenantSettings) error {
	if s.settings == nil {
		s.settings = make(map[string]*repository.TenantSettings)
	}
	s.settings[settings.TenantID] = &settings
	return nil
}

func adminGatewayHeaders(req *http.Request) {
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "admin-1")
	req.Header.Set("X-Tenant-Id", "tenant-test")
	req.Header.Set("X-Permissions", "ai.assistant.use,ai.admin")
}

func nonAdminGatewayHeaders(req *http.Request) {
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-test")
	req.Header.Set("X-Permissions", "ai.assistant.use")
}

func TestSettings_RequiresAdminPermission(t *testing.T) {
	store := &fakeSettingsStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings", nil)
	nonAdminGatewayHeaders(req)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin, got %d", res.Code)
	}
}

func TestSettings_GetAndUpsertMaskedKey(t *testing.T) {
	store := &fakeSettingsStore{
		settings: map[string]*repository.TenantSettings{
			"tenant-test": {
				TenantID:     "tenant-test",
				ProviderType: "openai",
				BaseURL:      "https://api.openai.com/v1",
				APIKey:       "sk-proj-test-provider-key-abcd89",
				ModelID:      "gpt-4o",
				Temperature:  0.2,
				IsActive:     true,
			},
		},
	}
	router := NewRouterWithOptions(store, nil, RouterOptions{})

	// 1. GET Settings
	getReq := httptest.NewRequest(http.MethodGet, "/api/ai/settings", nil)
	adminGatewayHeaders(getReq)
	getRes := httptest.NewRecorder()
	router.ServeHTTP(getRes, getReq)

	if getRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRes.Code, getRes.Body.String())
	}

	var getEnvelope struct {
		Result settingsDTO `json:"result"`
	}
	if err := json.Unmarshal(getRes.Body.Bytes(), &getEnvelope); err != nil {
		t.Fatalf("decode GET response failed: %v", err)
	}

	maskedKey := getEnvelope.Result.APIKey
	if !strings.Contains(maskedKey, "...") || strings.Contains(maskedKey, "provider-key") {
		t.Fatalf("apiKey must be masked, got: %s", maskedKey)
	}
	if getEnvelope.Result.ModelID != "gpt-4o" {
		t.Fatalf("expected modelId 'gpt-4o', got %v", getEnvelope.Result.ModelID)
	}

	// 2. PUT Settings
	updatePayload := `{"providerType":"openrouter","baseUrl":"https://openrouter.ai/api/v1","apiKey":"sk-or-new-test-key-efgh90","modelId":"anthropic/claude-3.5-sonnet","temperature":0.3}`
	putReq := httptest.NewRequest(http.MethodPut, "/api/ai/settings", strings.NewReader(updatePayload))
	adminGatewayHeaders(putReq)
	putRes := httptest.NewRecorder()
	router.ServeHTTP(putRes, putReq)

	if putRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on PUT, got %d: %s", putRes.Code, putRes.Body.String())
	}

	saved := store.settings["tenant-test"]
	if saved.ModelID != "anthropic/claude-3.5-sonnet" || saved.ProviderType != "openrouter" {
		t.Fatalf("settings not saved properly: %+v", saved)
	}
}

func TestSettings_TestConnection(t *testing.T) {
	// Mock OpenAI upstream server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer mockServer.Close()

	store := &fakeSettingsStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})

	// Test with correct key
	testBody := `{"baseUrl":"` + mockServer.URL + `","apiKey":"test-key","modelId":"gpt-4o-mini"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/test", strings.NewReader(testBody))
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var testEnvelope struct {
		Result testConnectionResponse `json:"result"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &testEnvelope)
	if !testEnvelope.Result.Success {
		t.Fatalf("expected test connection success, got error: %s", testEnvelope.Result.Error)
	}

	// Test with bad key
	badBody := `{"baseUrl":"` + mockServer.URL + `","apiKey":"wrong-key","modelId":"gpt-4o-mini"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/ai/settings/test", strings.NewReader(badBody))
	adminGatewayHeaders(req2)
	res2 := httptest.NewRecorder()
	router.ServeHTTP(res2, req2)

	var badEnvelope struct {
		Result testConnectionResponse `json:"result"`
	}
	_ = json.Unmarshal(res2.Body.Bytes(), &badEnvelope)
	if badEnvelope.Result.Success {
		t.Fatal("expected test connection failure for wrong key, got success")
	}
	if !strings.Contains(badEnvelope.Result.Error, "401") {
		t.Fatalf("expected HTTP 401 in error message, got: %s", badEnvelope.Result.Error)
	}
}
