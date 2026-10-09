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

func (s *fakeProfileStore) CreateProfile(_ context.Context, tenantID, name, providerType, baseURL, apiKey string, models []string, opts repository.ProfileOptions) (*repository.AIModelProfile, error) {
	profile := repository.AIModelProfile{ID: "p1", TenantID: tenantID, Name: name, ProviderType: providerType, BaseURL: baseURL, APIKey: apiKey, APIFormat: opts.APIFormat}
	if opts.ReasoningEffort != nil {
		profile.ReasoningEffort = *opts.ReasoningEffort
	}
	if opts.ReasoningBudget != nil {
		profile.ReasoningBudget = *opts.ReasoningBudget
	}
	for i, m := range models {
		profile.Models = append(profile.Models, repository.AIModelProfileModel{ID: fmt.Sprintf("m%d", i), ModelID: m})
	}
	s.profiles[tenantID] = append(s.bucket(tenantID), profile)
	return &profile, nil
}

func (s *fakeProfileStore) UpdateProfile(ctx context.Context, tenantID, profileID, name, providerType, baseURL, apiKey string, opts repository.ProfileOptions) (*repository.AIModelProfile, error) {
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
		if providerType != "" {
			items[i].ProviderType = providerType
		}
		if apiKey != "" {
			items[i].APIKey = apiKey
		}
		if opts.APIFormat != "" {
			items[i].APIFormat = opts.APIFormat
		}
		if opts.ReasoningEffort != nil {
			items[i].ReasoningEffort = *opts.ReasoningEffort
		}
		if opts.ReasoningBudget != nil {
			items[i].ReasoningBudget = *opts.ReasoningBudget
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

func createProfileRequest(t *testing.T, router http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles", strings.NewReader(body))
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func TestProfiles_APIFormatAndReasoningEffortRoundTrip(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{})

	res := createProfileRequest(t, router, `{"name":"Zen","baseUrl":"https://opencode.ai/zen/v1","apiKey":"k","apiFormat":"anthropic_messages","reasoningEffort":"high","models":["claude-sonnet-5-5"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var created struct {
		Result struct {
			Profile profileDTO `json:"profile"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Result.Profile.APIFormat != "anthropic_messages" || created.Result.Profile.ReasoningEffort != "high" {
		t.Fatalf("format/effort not returned: %+v", created.Result.Profile)
	}
}

// Profiles created before the column existed carry no format; the API must
// report the default instead of an empty string.
func TestProfiles_MissingAPIFormatReportsDefault(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{})
	res := createProfileRequest(t, router, `{"name":"Legacy","baseUrl":"https://api.openai.com/v1","apiKey":"k","models":["gpt-4o"]}`)
	if !strings.Contains(res.Body.String(), `"apiFormat":"chat_completions"`) {
		t.Fatalf("expected the default format, got %s", res.Body.String())
	}
}

func TestProfiles_RejectUnknownAPIFormatAndEffort(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{})
	for _, body := range []string{
		`{"name":"A","baseUrl":"https://api.openai.com/v1","apiKey":"k","apiFormat":"grpc"}`,
		`{"name":"B","baseUrl":"https://api.openai.com/v1","apiKey":"k","reasoningEffort":"extreme"}`,
	} {
		if res := createProfileRequest(t, router, body); res.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", body, res.Code)
		}
	}
}

func (s *fakeProfileStore) SetProfileModelFormat(_ context.Context, tenantID, profileID, modelID, apiFormat string) (*repository.AIModelProfile, error) {
	items := s.bucket(tenantID)
	for i := range items {
		if items[i].ID != profileID {
			continue
		}
		for j := range items[i].Models {
			if items[i].Models[j].ModelID == modelID {
				items[i].Models[j].APIFormat = apiFormat
				return &items[i], nil
			}
		}
		return nil, repository.ErrModelNotFound
	}
	return nil, repository.ErrModelProfileNotFound
}

func TestProfiles_ReasoningBudgetValidationAndRoundTrip(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{})
	if res := createProfileRequest(t, router, `{"name":"A","baseUrl":"https://api.anthropic.com","apiKey":"k","apiFormat":"anthropic_messages","reasoningBudgetTokens":500}`); res.Code != http.StatusBadRequest {
		t.Fatalf("a budget below the Anthropic minimum must be rejected, got %d", res.Code)
	}
	res := createProfileRequest(t, router, `{"name":"B","baseUrl":"https://api.anthropic.com","apiKey":"k","apiFormat":"anthropic_messages","reasoningBudgetTokens":12000,"models":["claude-sonnet-5-5"]}`)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"reasoningBudgetTokens":12000`) {
		t.Fatalf("budget not returned: %d %s", res.Code, res.Body.String())
	}
}

func TestProfiles_GeminiFormatAccepted(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{})
	res := createProfileRequest(t, router, `{"name":"G","baseUrl":"https://opencode.ai/zen/v1","apiKey":"k","apiFormat":"google_gemini","models":["gemini-3.8-flash"]}`)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"apiFormat":"google_gemini"`) {
		t.Fatalf("gemini format: %d %s", res.Code, res.Body.String())
	}
}

// Models carry a server-side format suggestion so every client hints the same.
func TestProfiles_ModelsCarryFormatSuggestionAndOverride(t *testing.T) {
	store := &fakeProfileStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})
	res := createProfileRequest(t, router, `{"name":"Zen","baseUrl":"https://opencode.ai/zen/v1","apiKey":"k","models":["claude-opus-5","deepseek-v4-pro"]}`)
	if !strings.Contains(res.Body.String(), `"suggestedApiFormat":"anthropic_messages"`) || !strings.Contains(res.Body.String(), `"suggestedApiFormat":"chat_completions"`) {
		t.Fatalf("suggestions missing: %s", res.Body.String())
	}

	patch := func(modelID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/ai/settings/profiles/p1/models/"+modelID, strings.NewReader(body))
		adminGatewayHeaders(req)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	if out := patch("claude-opus-5", `{"apiFormat":"anthropic_messages"}`); out.Code != http.StatusOK || !strings.Contains(out.Body.String(), `"apiFormat":"anthropic_messages"`) {
		t.Fatalf("override not applied: %d %s", out.Code, out.Body.String())
	}
	if out := patch("claude-opus-5", `{"apiFormat":"soap"}`); out.Code != http.StatusBadRequest {
		t.Fatalf("an unknown format must be rejected, got %d", out.Code)
	}
	if out := patch("missing-model", `{"apiFormat":"chat_completions"}`); out.Code != http.StatusNotFound {
		t.Fatalf("an unknown model must 404, got %d", out.Code)
	}
}

func TestProfiles_AddModelsWithFormats(t *testing.T) {
	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{})
	createProfileRequest(t, router, `{"name":"Zen","baseUrl":"https://opencode.ai/zen/v1","apiKey":"k","models":["glm-5.3"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles/p1/models", strings.NewReader(`{"models":["gpt-5.5"],"formats":{"gpt-5.5":"openai_responses"}}`))
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"apiFormat":"openai_responses"`) {
		t.Fatalf("format on add not stored: %d %s", res.Code, res.Body.String())
	}
}

func TestProfiles_AvailableModelsListsProviderModels(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer secret-key" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.5"},{"id":"glm-5.3"},{"id":"jev-1.13"}]}`))
	}))
	defer provider.Close()

	router := NewRouterWithOptions(&fakeProfileStore{}, nil, RouterOptions{AllowLocalModelURLs: true})
	createProfileRequest(t, router, `{"name":"Local","baseUrl":"`+provider.URL+`/v1","apiKey":"secret-key","models":["glm-5.3"]}`)

	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/profiles/p1/available-models", nil)
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	body := res.Body.String()
	if res.Code != http.StatusOK || strings.Contains(body, "jev-1.13") || !strings.Contains(body, `"modelId":"gpt-5.5"`) {
		t.Fatalf("unexpected list: %d %s", res.Code, body)
	}
	if !strings.Contains(body, `"suggestedApiFormat":"openai_responses"`) || !strings.Contains(body, `"added":true`) {
		t.Fatalf("suggestion or added flag missing: %s", body)
	}
	if strings.Contains(body, "secret-key") {
		t.Fatalf("the key must never be echoed: %s", body)
	}
}

func TestProfiles_AvailableModelsRefusesDisallowedHost(t *testing.T) {
	// A profile stored before the allowlist existed must still not be contacted.
	store := &fakeProfileStore{profiles: map[string][]repository.AIModelProfile{
		"tenant-test": {{ID: "p1", TenantID: "tenant-test", Name: "x", BaseURL: "https://evil.example/v1", APIKey: "k", ProviderType: "openai-compatible"}},
	}}
	router := NewRouterWithOptions(store, nil, RouterOptions{ModelBaseURLAllowlist: []string{"https://ai-gateway.arda.io.vn/compat"}})
	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/profiles/p1/available-models", nil)
	adminGatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if !strings.Contains(res.Body.String(), "danh sách được phép") {
		t.Fatalf("an off-allowlist host must not be contacted: %d %s", res.Code, res.Body.String())
	}
}
