package decision

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsValidAcceptsConfiguredDecisionModels(t *testing.T) {
	for _, model := range []string{"jev-1.13-free", "jev-1.13", "jev-latest", "typesafe/jev-2", "custom.Model_v1"} {
		if !(Settings{ModelID: model, MinConfidence: 0.8}).Valid() {
			t.Fatalf("model %q rejected by Valid()", model)
		}
	}
	for _, model := range []string{"", "   ", "-leading", ".dot", "has space", "bad\nmodel", strings.Repeat("a", 129)} {
		if (Settings{ModelID: model, MinConfidence: 0.8}).Valid() {
			t.Fatalf("model %q accepted by Valid()", model)
		}
	}
}

func TestSettingsValidBoundsMinConfidence(t *testing.T) {
	settings := Settings{ModelID: DefaultModel, MinConfidence: 0.5}
	if !settings.Valid() {
		t.Fatal("0.5 confidence rejected")
	}
	settings.MinConfidence = 1
	if !settings.Valid() {
		t.Fatal("1.0 confidence rejected")
	}
	for _, value := range []float64{0.49, 1.01, math.NaN()} {
		settings.MinConfidence = value
		if settings.Valid() {
			t.Fatalf("confidence %v accepted", value)
		}
	}
}

func TestResultSkillRequiresConfidenceForSpecializedRouting(t *testing.T) {
	result := &Result{Answers: map[string]Answer{
		"skill":        ChoiceAnswer("report", 0.9, map[string]float64{"report": 0.95, "knowledge": 0.04, "general": 0.01}),
		"report_topic": ChoiceAnswer("loan_portfolio", 0.85, map[string]float64{"loan_portfolio": 0.9, "other": 0.1}),
	}}
	if got := result.Skill(0.8); got != "loan_portfolio" {
		t.Fatalf("skill = %q, want loan_portfolio", got)
	}
	if got := result.Skill(0.9); got != "report" {
		t.Fatalf("skill = %q, want report when topic confidence is below threshold", got)
	}
	result.Answers["skill"] = ChoiceAnswer("report", 0.7, map[string]float64{"report": 0.8, "knowledge": 0.1, "general": 0.1})
	if got := result.Skill(0.8); got != "general" {
		t.Fatalf("skill = %q, want general for low confidence", got)
	}
	if confidence := result.ChoiceConfidence("skill"); confidence != 0.7 {
		t.Fatalf("ChoiceConfidence = %v, want 0.7", confidence)
	}
}

func TestResultSkillRejectsMalformedProbabilities(t *testing.T) {
	result := &Result{Answers: map[string]Answer{
		"skill": ChoiceAnswer("report", 0.9, map[string]float64{"report": 1, "knowledge": 0}),
	}}
	if got := result.Skill(0.8); got != "general" {
		t.Fatalf("skill = %q, want general for malformed choice", got)
	}
	if confidence := result.ChoiceConfidence("missing"); confidence != 0 {
		t.Fatalf("missing ChoiceConfidence = %v, want 0", confidence)
	}
}

func testServer(t *testing.T, response string, capture *map[string]any) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/systemone" {
			t.Errorf("path = %q, want /systemone", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if capture != nil {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
			}
			*capture = body
		}
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return &Client{http: &http.Client{Timeout: Timeout}, baseURL: server.URL}
}

func testSettings() Settings {
	return Settings{Enabled: true, ModelID: DefaultModel, MinConfidence: 0.8, APIKey: "test-key"}
}

func TestEvaluateQuestionsDecodesNoul(t *testing.T) {
	var body map[string]any
	client := testServer(t, `{"model":"jev-test","answers":{"grounded":{"type":"noul","noul":0.93}},"usage":{"input_tokens":12,"output_tokens":1}}`, &body)

	result, err := client.EvaluateQuestions(context.Background(), testSettings(), "state",
		map[string]Question{"grounded": NoulQuestion("Is the answer grounded?", NoulCriteria{True: "supported", False: "unsupported"})})
	if err != nil {
		t.Fatalf("EvaluateQuestions: %v", err)
	}
	value, ok := result.Noul("grounded")
	if !ok || value != 0.93 {
		t.Fatalf("Noul = %v ok=%v, want 0.93", value, ok)
	}
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 1 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	questions, _ := body["questions"].(map[string]any)
	question, _ := questions["grounded"].(map[string]any)
	if question["type"] != "noul" {
		t.Fatalf("question type = %v", question["type"])
	}
	criteria, _ := question["criteria"].(map[string]any)
	if criteria["true"] != "supported" || criteria["false"] != "unsupported" {
		t.Fatalf("unexpected criteria: %+v", criteria)
	}
}

func TestEvaluateRoutingPayloadShapeUnchanged(t *testing.T) {
	var body map[string]any
	client := testServer(t, `{"model":"jev-test","answers":{"skill":{"type":"choice","choice":"report","probabilities":{"report":0.9,"knowledge":0.05,"general":0.05},"confidence":0.85},"report_topic":{"type":"choice","choice":"other","probabilities":{"loan_portfolio":0.1,"other":0.9},"confidence":0.8}},"usage":{"input_tokens":20,"output_tokens":3}}`, &body)

	result, err := client.Evaluate(context.Background(), testSettings(), "Latest user request: test")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Skill(0.8) != "report" {
		t.Fatalf("skill = %q, want report", result.Skill(0.8))
	}
	if body["model"] != DefaultModel || body["state"] != "Latest user request: test" {
		t.Fatalf("unexpected payload: %+v", body)
	}
	questions, _ := body["questions"].(map[string]any)
	if len(questions) != 2 {
		t.Fatalf("questions = %d, want 2", len(questions))
	}
	skill, _ := questions["skill"].(map[string]any)
	if skill["type"] != "choice" {
		t.Fatalf("skill question type = %v", skill["type"])
	}
	criteria, _ := skill["criteria"].(map[string]any)
	if len(criteria) != 3 || criteria["report"] == nil {
		t.Fatalf("unexpected skill criteria: %+v", criteria)
	}
}

func TestEvaluateQuestionsRejectsMalformedAnswers(t *testing.T) {
	cases := []struct {
		name     string
		response string
	}{
		{"noul out of range", `{"model":"m","answers":{"q":{"type":"noul","noul":1.5}},"usage":{}}`},
		{"noul missing", `{"model":"m","answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":1},"confidence":1}},"usage":{}}`},
		{"missing answer", `{"model":"m","answers":{},"usage":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testServer(t, tc.response, nil)
			_, err := client.EvaluateQuestions(context.Background(), testSettings(), "state",
				map[string]Question{"q": NoulQuestion("yes/no?", NoulCriteria{True: "yes", False: "no"})})
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestEvaluateQuestionsRejectsUnsupportedQuestionType(t *testing.T) {
	client := testServer(t, `{"model":"m","answers":{"q":{"type":"score"}},"usage":{}}`, nil)
	_, err := client.EvaluateQuestions(context.Background(), testSettings(), "state",
		map[string]Question{"q": {Type: "score", Instructions: "rate"}})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestEvaluateQuestionsRequiresConfiguration(t *testing.T) {
	client := NewClient(nil)
	questions := map[string]Question{"q": NoulQuestion("yes/no?", NoulCriteria{True: "yes", False: "no"})}
	if _, err := client.EvaluateQuestions(context.Background(), Settings{}, "state", questions); err == nil {
		t.Fatal("unconfigured settings accepted")
	}
	settings := testSettings()
	settings.APIKey = ""
	if _, err := client.EvaluateQuestions(context.Background(), settings, "state", questions); err == nil {
		t.Fatal("missing API key accepted")
	}
	if _, err := client.EvaluateQuestions(context.Background(), testSettings(), "state", nil); err == nil {
		t.Fatal("empty question set accepted")
	}
	oversized := strings.Repeat("x", 8193)
	if _, err := client.EvaluateQuestions(context.Background(), testSettings(), oversized, questions); err == nil {
		t.Fatal("oversized state accepted")
	}
}
