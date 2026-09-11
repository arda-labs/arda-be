package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeProfileStore struct {
	fakeToolRunStore
	profiles map[string][]repository.AIModelProfile
}

func (s *fakeProfileStore) bucket(tenantID string) []repository.AIModelProfile {
	if s.profiles == nil {
		s.profiles = map[string][]repository.AIModelProfile{}
	}
	return s.profiles[tenantID]
}

func (s *fakeProfileStore) ListProfiles(_ context.Context, tenantID string) ([]repository.AIModelProfile, error) {
	return s.bucket(tenantID), nil
}

func (s *fakeProfileStore) CreateProfile(_ context.Context, tenantID, name, baseURL, apiKey string, models []string) (*repository.AIModelProfile, error) {
	profile := repository.AIModelProfile{ID: "p1", TenantID: tenantID, Name: name, BaseURL: baseURL, APIKey: apiKey}
	for i, m := range models {
		profile.Models = append(profile.Models, repository.AIModelProfileModel{ID: fmt.Sprintf("m%d", i), ModelID: m})
	}
	s.profiles[tenantID] = append(s.bucket(tenantID), profile)
	return &profile, nil
}

func (s *fakeProfileStore) UpdateProfile(ctx context.Context, tenantID, profileID, name, baseURL, apiKey string) (*repository.AIModelProfile, error) {
	items := s.bucket(tenantID)
	for i := range items {
		if items[i].ID != profileID {
			continue
		}
		if name != "" {
			items[i].Name = name
		}
		if baseURL != "" {
			items[i].BaseURL = baseURL
		}
		if apiKey != "" {
			items[i].APIKey = apiKey
		}
		return &items[i], nil
	}
	return nil, repository.ErrModelProfileNotFound
}

func (s *fakeProfileStore) DeleteProfile(_ context.Context, tenantID, profileID string) error {
	items := s.bucket(tenantID)
	for i := range items {
		if items[i].ID == profileID {
			s.profiles[tenantID] = append(items[:i], items[i+1:]...)
			return nil
		}
	}
	return repository.ErrModelProfileNotFound
}

func (s *fakeProfileStore) AddProfileModels(_ context.Context, tenantID, profileID string, models []string) (*repository.AIModelProfile, error) {
	items := s.bucket(tenantID)
	for i := range items {
		if items[i].ID != profileID {
			continue
		}
		for _, m := range models {
			items[i].Models = append(items[i].Models, repository.AIModelProfileModel{ID: "m-" + m, ModelID: m})
		}
		return &items[i], nil
	}
	return nil, repository.ErrModelProfileNotFound
}

func (s *fakeProfileStore) DeleteProfileModel(_ context.Context, tenantID, profileID, modelID string) error {
	items := s.bucket(tenantID)
	for i := range items {
		if items[i].ID != profileID {
			continue
		}
		for j := range items[i].Models {
			if items[i].Models[j].ModelID == modelID {
				items[i].Models = append(items[i].Models[:j], items[i].Models[j+1:]...)
				return nil
			}
		}
	}
	return repository.ErrModelNotFound
}

func (s *fakeProfileStore) ApplyProfileModel(_ context.Context, tenantID, profileID, modelID string) (*repository.AIModelProfile, error) {
	items := s.bucket(tenantID)
	for i := range items {
		items[i].IsActive = items[i].ID == profileID
		for j := range items[i].Models {
			items[i].Models[j].IsActive = items[i].ID == profileID && items[i].Models[j].ModelID == modelID
			if items[i].Models[j].IsActive {
				return &items[i], nil
			}
		}
	}
	return nil, repository.ErrModelNotFound
}

func TestProfiles_RequiresGatewayIdentityContext(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{})
	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/profiles", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestProfiles_CreateValidatesBaseURLAgainstAllowlist(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{
		ModelBaseURLAllowlist: []string{"https://gateway.example/v1/acct/gw"},
	})
	body := `{"name":"Prod","baseUrl":"https://evil.example/v1","apiKey":"sk-secret","models":["gpt-4o"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles", strings.NewReader(body))
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-allowlist URL, got %d: %s", res.Code, res.Body.String())
	}
}

func TestProfiles_CreateMasksKeyAndLists(t *testing.T) {
	store := &fakeProfileStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})

	body := `{"name":"Prod","baseUrl":"https://api.openai.com/v1","apiKey":"sk-super-secret-key","models":["gpt-4o","gpt-4o-mini"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles", strings.NewReader(body))
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("create profile: expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "sk-super-secret-key") {
		t.Fatalf("response leaked raw API key: %s", res.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/ai/settings/profiles", nil)
	adminGatewayHeaders(listReq)
	listRes := httptest.NewRecorder()
	router.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list profiles: expected 200, got %d", listRes.Code)
	}
	var envelope struct {
		Result struct {
			Profiles []profileDTO `json:"profiles"`
		} `json:"result"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(envelope.Result.Profiles) != 1 || len(envelope.Result.Profiles[0].Models) != 2 {
		t.Fatalf("expected 1 profile with 2 models, got %+v", envelope.Result.Profiles)
	}
	if !envelope.Result.Profiles[0].HasAPIKey {
		t.Fatalf("expected hasApiKey true")
	}
}

func TestProfiles_ApplySelectsOneModel(t *testing.T) {
	store := &fakeProfileStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})
	createReq := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles",
		strings.NewReader(`{"name":"Prod","baseUrl":"https://api.openai.com/v1","apiKey":"sk-test-secret-key-abc","models":["gpt-4o","gpt-4o-mini"]}`))
	adminGatewayHeaders(createReq)
	router.ServeHTTP(httptest.NewRecorder(), createReq)

	applyReq := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles/p1/apply", strings.NewReader(`{"modelId":"gpt-4o-mini"}`))
	adminGatewayHeaders(applyReq)
	applyRes := httptest.NewRecorder()
	router.ServeHTTP(applyRes, applyReq)
	if applyRes.Code != http.StatusOK {
		t.Fatalf("apply model: expected 200, got %d: %s", applyRes.Code, applyRes.Body.String())
	}
	var envelope struct {
		Result struct {
			Profile profileDTO `json:"profile"`
		} `json:"result"`
	}
	if err := json.Unmarshal(applyRes.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	active := 0
	for _, m := range envelope.Result.Profile.Models {
		if m.IsActive {
			active++
			if m.ModelID != "gpt-4o-mini" {
				t.Fatalf("expected gpt-4o-mini active, got %s", m.ModelID)
			}
		}
	}
	if active != 1 {
		t.Fatalf("expected exactly one active model, got %d", active)
	}
}

func TestProfiles_DeleteModelNotFound(t *testing.T) {
	store := &fakeProfileStore{profiles: map[string][]repository.AIModelProfile{
		"tenant-test": {{ID: "p1", TenantID: "tenant-test", Name: "Prod"}},
	}}
	router := NewRouterWithOptions(store, nil, RouterOptions{})
	req := httptest.NewRequest(http.MethodDelete, "/api/ai/settings/profiles/p1/models/missing", nil)
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing model, got %d", res.Code)
	}
}
