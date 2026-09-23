package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeDecisionStore struct {
	fakeToolRunStore
	settings decision.Settings
	saved    bool
}

func (s *fakeDecisionStore) GetDecisionSettings(_ context.Context, _ string) (decision.Settings, error) {
	return s.settings, nil
}

func (s *fakeDecisionStore) SaveDecisionSettings(_ context.Context, _ string, settings decision.Settings, _ *string) error {
	s.settings, s.saved = settings, true
	return nil
}

type fakeDecisionEvaluator struct {
	result *decision.Result
	state  string
}

func (e *fakeDecisionEvaluator) Evaluate(_ context.Context, _ decision.Settings, state string) (*decision.Result, error) {
	e.state = state
	return e.result, nil
}

func routedDecisionResult(skill, topic string, skillConfidence, topicConfidence float64) *decision.Result {
	return &decision.Result{Model: decision.DefaultModel, Answers: map[string]decision.Answer{
		"skill":        decision.ChoiceAnswer(skill, skillConfidence, map[string]float64{"report": skillConfidence, "knowledge": (1 - skillConfidence) / 2, "general": (1 - skillConfidence) / 2}),
		"report_topic": decision.ChoiceAnswer(topic, topicConfidence, map[string]float64{"loan_portfolio": topicConfidence, "other": 1 - topicConfidence}),
	}}
}

func TestDecisionSettingsGetDoesNotExposeAPIKey(t *testing.T) {
	store := &fakeDecisionStore{settings: decision.Settings{Enabled: true, ModelID: decision.DefaultModel, MinConfidence: 0.8, APIKey: "secret"}}
	router := NewRouterWithOptions(store, nil, RouterOptions{})
	req := httptest.NewRequest(http.MethodGet, "/api/ai/settings/decision", nil)
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "secret") || !strings.Contains(res.Body.String(), `"has_api_key":true`) {
		t.Fatalf("unexpected response: %d %s", res.Code, res.Body.String())
	}
}

func TestDecisionSettingsTestUsesDraftWithoutSaving(t *testing.T) {
	store := &fakeDecisionStore{settings: decision.Defaults()}
	evaluator := &fakeDecisionEvaluator{result: routedDecisionResult("report", "loan_portfolio", .9, .9)}
	router := NewRouterWithOptions(store, nil, RouterOptions{DecisionEvaluator: evaluator})
	body := `{"enabled":true,"model_id":"jev-1.13-free","min_confidence":0.8,"api_key":"key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/decision/test", strings.NewReader(body))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || store.saved || !strings.Contains(res.Body.String(), `"skill":"loan_portfolio"`) || strings.Contains(res.Body.String(), `"api_key":"key"`) {
		t.Fatalf("unexpected response: %d %s", res.Code, res.Body.String())
	}
}

func TestRouteDecisionAddsOnlyServerOwnedLoanPortfolioSkill(t *testing.T) {
	store := &fakeDecisionStore{settings: decision.Settings{Enabled: true, ModelID: decision.DefaultModel, MinConfidence: .8, APIKey: "key"}}
	evaluator := &fakeDecisionEvaluator{result: routedDecisionResult("report", "loan_portfolio", .9, .9)}
	messages := []model.Message{{Role: "system", Content: "base"}, {Role: "user", Content: "Phân tích dư nợ theo nhóm nợ kỳ 2026-08"}}
	skill, routed := routeDecision(context.Background(), store, RouterOptions{DecisionEvaluator: evaluator}, repository.RunContext{TenantID: "tenant-1", ExternalRun: "r1"}, messages)
	if skill != "loan_portfolio" || len(routed) != 3 || !strings.Contains(routed[1].Content, "LOAN_PORTFOLIO") || routed[2].Role != "user" {
		t.Fatalf("unexpected routing result: skill=%q messages=%+v", skill, routed)
	}
	if strings.Contains(routed[1].Content, "key") || !strings.Contains(evaluator.state, "Latest user request") {
		t.Fatalf("unexpected decision state or instruction: state=%q instruction=%q", evaluator.state, routed[1].Content)
	}
}

func TestChatProfilesRejectDecisionModel(t *testing.T) {
	store := &fakeProfileStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})
	body := `{"name":"Wrong purpose","baseUrl":"https://opencode.ai/zen/v1","apiKey":"key","models":["jev-1.13-free"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/settings/profiles", strings.NewReader(body))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "ai.model_purpose_mismatch") {
		t.Fatalf("expected purpose mismatch, got %d: %s", res.Code, res.Body.String())
	}
}
