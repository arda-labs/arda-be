package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"

	"github.com/arda-labs/arda/apps/ai-service/internal/evaluation"
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

const jobSecret = "01234567890123456789012345678901"

func writeSet(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "set.yaml")
	if err := os.WriteFile(p, []byte("version: 1\ncases:\n  - id: c1\n    query: hello\n    expected:\n      source_keys: [doc-a]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func callEval(t *testing.T, q evaluation.QueryFunc, source string) (int, map[string]any) {
	t.Helper()
	return callEvalWithSet(t, writeSet(t), q, source)
}

func callEvalWithSet(t *testing.T, setPath string, q evaluation.QueryFunc, source string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	NewInternalJobsHandler(setPath, q).Register(mux, jobSecret)
	req := httptest.NewRequest("POST", "/internal/jobs/ai-eval", nil)
	if source != "" {
		tok, _ := identity.Issue(jobSecret, source, "ai-service", time.Now(), time.Minute)
		req.Header.Set(identity.MetadataKey, tok)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAIEvalJobPassAndFail(t *testing.T) {
	pass := func(context.Context, evaluation.Case) (knowledge.QueryResponse, error) {
		return knowledge.QueryResponse{Hits: []knowledge.QueryHit{{SourceKey: "doc-a", Citation: "doc-a#1"}}}, nil
	}
	code, out := callEval(t, pass, "platform-service")
	if code != 200 || out["status"] != "succeeded" {
		t.Fatalf("pass: %d %v", code, out)
	}
	fail := func(context.Context, evaluation.Case) (knowledge.QueryResponse, error) {
		return knowledge.QueryResponse{}, errors.New("rag query failed")
	}
	code, out = callEval(t, fail, "platform-service")
	if code != 200 || out["status"] != "failed" || out["error_code"] != "AI_EVAL_FAILED" || out["error_message"] != "1/1 case failed" {
		t.Fatalf("fail: %d %v", code, out)
	}
}

func TestAIEvalJobRequiresPlatformSignature(t *testing.T) {
	ok := func(context.Context, evaluation.Case) (knowledge.QueryResponse, error) {
		return knowledge.QueryResponse{}, nil
	}
	if code, _ := callEval(t, ok, ""); code != http.StatusUnauthorized {
		t.Fatalf("unsigned must be 401, got %d", code)
	}
	if code, _ := callEval(t, ok, "auth-gateway"); code != http.StatusForbidden {
		t.Fatalf("wrong source must be 403, got %d", code)
	}
}

func TestAIEvalJobEmptySetFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.yaml")
	if err := os.WriteFile(p, []byte("version: 1\ncases: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	q := func(context.Context, evaluation.Case) (knowledge.QueryResponse, error) {
		return knowledge.QueryResponse{}, nil
	}
	code, out := callEvalWithSet(t, p, q, "platform-service")
	if code != 200 || out["status"] != "failed" || out["error_code"] != "AI_EVAL_EMPTY_SET" {
		t.Fatalf("empty set: %d %v", code, out)
	}
}
