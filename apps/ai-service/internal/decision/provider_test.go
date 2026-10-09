package decision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeProviderAndEndpoints(t *testing.T) {
	cases := map[string]string{
		"": ProviderOpenCodeZen, "opencode-zen": ProviderOpenCodeZen, " TypeSafe ": ProviderTypeSafe,
	}
	for in, want := range cases {
		if got, ok := NormalizeProvider(in); !ok || got != want {
			t.Errorf("NormalizeProvider(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := NormalizeProvider("openai"); ok {
		t.Error("only System One providers are accepted")
	}
	if BaseURLFor(ProviderTypeSafe) != "https://api.typesafe.ai/v1" || BaseURLFor("") != BaseURL {
		t.Errorf("unexpected endpoints: %q / %q", BaseURLFor(ProviderTypeSafe), BaseURLFor(""))
	}
	if DefaultModelFor(ProviderTypeSafe) != "jev-latest" || DefaultModelFor(ProviderOpenCodeZen) != DefaultModel {
		t.Error("unexpected default models")
	}
}

func TestSettingsValidRejectsUnknownProvider(t *testing.T) {
	base := Settings{ModelID: "jev-latest", MinConfidence: 0.8}
	if !base.Valid() {
		t.Fatal("an empty provider means the default and must stay valid")
	}
	base.Provider = ProviderTypeSafe
	if !base.Valid() {
		t.Fatal("typesafe must be valid")
	}
	base.Provider = "somewhere-else"
	if base.Valid() {
		t.Fatal("an unknown provider must be rejected")
	}
}

// The tenant's provider decides which host receives the credential.
func TestEvaluateQuestionsFollowsProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":0.5}},"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	client := NewClient(nil)
	if got := client.endpoint(Settings{Provider: ProviderTypeSafe}); got != "https://api.typesafe.ai/v1" {
		t.Fatalf("typesafe endpoint = %q", got)
	}
	if got := client.endpoint(Settings{}); got != BaseURL {
		t.Fatalf("default endpoint = %q", got)
	}
	client.baseURL = server.URL
	if got := client.endpoint(Settings{Provider: ProviderTypeSafe}); got != server.URL {
		t.Fatalf("an explicit base URL must win (test seam), got %q", got)
	}
}

func TestScoreQuestionDecodesAndValidates(t *testing.T) {
	question := ScoreQuestion("How frustrated?", []string{"calm", "frustrated", "angry"})
	good := `{"model":"jev-1.13.0","answers":{"mood":{"type":"score","score":1.26,"confidence":0.61,"legend":{"0":"calm","1":"frustrated","2":"angry"},"probabilities":{"0":0.0,"1":0.74,"2":0.26}}},"usage":{"input_tokens":5,"output_tokens":2}}`
	client := testServer(t, good, nil)
	result, err := client.EvaluateQuestions(context.Background(), testSettings(), "state", map[string]Question{"mood": question})
	if err != nil {
		t.Fatalf("EvaluateQuestions: %v", err)
	}
	score, ok := result.ScoreOf("mood")
	if !ok || score.Value != 1.26 || score.Confidence != 0.61 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}

	for name, body := range map[string]string{
		"out of range": `{"model":"m","answers":{"mood":{"type":"score","score":3.5,"confidence":0.5,"probabilities":{"0":0.3,"1":0.3,"2":0.4}}},"usage":{"input_tokens":1,"output_tokens":1}}`,
		"bad sum":      `{"model":"m","answers":{"mood":{"type":"score","score":1,"confidence":0.5,"probabilities":{"0":0.1,"1":0.1,"2":0.1}}},"usage":{"input_tokens":1,"output_tokens":1}}`,
		"missing":      `{"model":"m","answers":{"mood":{"type":"score","confidence":0.5,"probabilities":{"0":0.3,"1":0.3,"2":0.4}}},"usage":{"input_tokens":1,"output_tokens":1}}`,
	} {
		bad := testServer(t, body, nil)
		if _, err := bad.EvaluateQuestions(context.Background(), testSettings(), "state", map[string]Question{"mood": question}); err == nil {
			t.Errorf("%s: a malformed score must invalidate the response", name)
		}
	}
}
