package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeProfileStore struct {
	fakeSettingsStore
	profiles  map[string]*repository.TenantSettingProfile
	activated string
}

func (s *fakeProfileStore) ListProfiles(_ context.Context, _ string) ([]repository.TenantSettingProfile, error) {
	items := make([]repository.TenantSettingProfile, 0, len(s.profiles))
	for _, profile := range s.profiles {
		items = append(items, *profile)
	}
	return items, nil
}

func (s *fakeProfileStore) CreateProfile(_ context.Context, profile repository.TenantSettingProfile) (*repository.TenantSettingProfile, error) {
	if s.profiles == nil {
		s.profiles = map[string]*repository.TenantSettingProfile{}
	}
	for _, existing := range s.profiles {
		if existing.Name == profile.Name {
			return nil, repository.ErrProfileNameExists
		}
	}
	profile.ID = "prof-" + profile.Name
	s.profiles[profile.ID] = &profile
	return &profile, nil
}

func (s *fakeProfileStore) DeleteProfile(_ context.Context, _, profileID string) error {
	if _, ok := s.profiles[profileID]; !ok {
		return repository.ErrProfileNotFound
	}
	delete(s.profiles, profileID)
	return nil
}

func (s *fakeProfileStore) ActivateProfile(_ context.Context, _, profileID string) (*repository.TenantSettingProfile, error) {
	profile, ok := s.profiles[profileID]
	if !ok {
		return nil, repository.ErrProfileNotFound
	}
	s.activated = profileID
	profile.IsActive = true
	return profile, nil
}

func TestProfilesLifecycle(t *testing.T) {
	store := &fakeProfileStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})

	// Create
	createReq := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles",
		strings.NewReader(`{"name":"zen-old","providerType":"openai","baseUrl":"https://opencode.ai/zen/v1","apiKey":"sk-old-key-abcdef","modelId":"x-preview-f-free"}`))
	adminGatewayHeaders(createReq)
	createRes := httptest.NewRecorder()
	router.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", createRes.Code, createRes.Body.String())
	}

	// Duplicate name → 409
	dupRes := httptest.NewRecorder()
	dupReq := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles",
		strings.NewReader(`{"name":"zen-old","baseUrl":"https://opencode.ai/zen/v1","apiKey":"sk-x-abcdef","modelId":"m"}`))
	adminGatewayHeaders(dupReq)
	router.ServeHTTP(dupRes, dupReq)
	if dupRes.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate name, got %d", dupRes.Code)
	}

	// List masks key
	listReq := httptest.NewRequest(http.MethodGet, "/api/ai/settings/profiles", nil)
	adminGatewayHeaders(listReq)
	listRes := httptest.NewRecorder()
	router.ServeHTTP(listRes, listReq)
	body := listRes.Body.String()
	if !strings.Contains(body, "zen-old") || !strings.Contains(body, "...") {
		t.Fatalf("list missing profile or key not masked: %s", body)
	}
	if strings.Contains(body, "sk-old-key") {
		t.Fatalf("api key leaked in list response: %s", body)
	}

	// Activate
	activateReq := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles/prof-zen-old/activate", nil)
	adminGatewayHeaders(activateReq)
	activateRes := httptest.NewRecorder()
	router.ServeHTTP(activateRes, activateReq)
	if activateRes.Code != http.StatusOK || store.activated != "prof-zen-old" {
		t.Fatalf("activate failed: %d %s", activateRes.Code, activateRes.Body.String())
	}

	// Delete
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/ai/settings/profiles/prof-zen-old", nil)
	adminGatewayHeaders(deleteReq)
	deleteRes := httptest.NewRecorder()
	router.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusOK {
		t.Fatalf("delete failed: %d", deleteRes.Code)
	}
	missingReq := httptest.NewRequest(http.MethodDelete, "/api/ai/settings/profiles/prof-zen-old", nil)
	adminGatewayHeaders(missingReq)
	missingRes := httptest.NewRecorder()
	router.ServeHTTP(missingRes, missingReq)
	if missingRes.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting missing profile, got %d", missingRes.Code)
	}
}

func TestProfilesCreateRejectsDisallowedBaseURL(t *testing.T) {
	store := &fakeProfileStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{
		ModelBaseURLAllowlist: []string{"https://gateway.example.com/v1/gw"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles",
		strings.NewReader(`{"name":"b.ai","baseUrl":"https://api.b.ai/v1","apiKey":"sk-key-abcdef","modelId":"deepseek-v4-flash"}`))
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "ai.base_url_not_allowed") {
		t.Fatalf("expected allowlist rejection, got %d %s", res.Code, res.Body.String())
	}
	if len(store.profiles) != 0 {
		t.Fatalf("profile must not be persisted when base URL rejected")
	}
}
