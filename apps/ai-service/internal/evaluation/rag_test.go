package evaluation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

func TestParseAndRunRAGSet(t *testing.T) {
	set, err := Parse([]byte("version: 1\ncases:\n  - id: known\n    tenant: t1\n    query: fee\n    expected:\n      answer_must_cite: true\n      source_keys: [fees]\n"))
	if err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), set, func(context.Context, Case) (knowledge.QueryResponse, error) {
		return knowledge.QueryResponse{LatencyMs: 4, Hits: []knowledge.QueryHit{{SourceKey: "fees", Citation: "[1]"}}}, nil
	})
	if report.Failed != 0 || report.RecallAtK != 1 || report.CitationCoverage != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunRAGSetFailsMissingEvidence(t *testing.T) {
	set := Set{Version: 1, Cases: []Case{{ID: "missing", Expected: Expected{AnswerMustCite: true, SourceKeys: []string{"fees"}}}}}
	report := Run(context.Background(), set, func(context.Context, Case) (knowledge.QueryResponse, error) { return knowledge.QueryResponse{}, nil })
	if report.Failed != 1 || report.Passed != 0 {
		t.Fatalf("expected failed case: %+v", report)
	}
}

func TestHTTPQueryCookiePropagation(t *testing.T) {
	t.Run("explicit cookie propagates Cookie header and preserves identity", func(t *testing.T) {
		var receivedCookie string
		var receivedAuthChecked string
		var receivedUserID string
		var receivedTenantID string
		var receivedPermissions string
		var receivedContentType string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedCookie = r.Header.Get("Cookie")
			receivedAuthChecked = r.Header.Get("X-Auth-Checked")
			receivedUserID = r.Header.Get("X-User-Id")
			receivedTenantID = r.Header.Get("X-Tenant-Id")
			receivedPermissions = r.Header.Get("X-Permissions")
			receivedContentType = r.Header.Get("Content-Type")

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(knowledge.QueryResponse{
				LatencyMs: 8,
				Hits: []knowledge.QueryHit{
					{SourceKey: "source-1", Citation: "[1]"},
				},
			})
		}))
		defer server.Close()

		q := HTTPQuery(server.URL, "test-user", "default-tenant", "ai.assistant.use,ai.knowledge.read", "arda_sid=sess_12345", server.Client())
		res, err := q(context.Background(), Case{Query: "policy query", Tenant: "case-tenant"})
		if err != nil {
			t.Fatalf("unexpected query error: %v", err)
		}
		if len(res.Hits) != 1 || res.Hits[0].SourceKey != "source-1" {
			t.Fatalf("unexpected hits: %+v", res.Hits)
		}

		if receivedCookie != "arda_sid=sess_12345" {
			t.Fatalf("expected Cookie header %q, got %q", "arda_sid=sess_12345", receivedCookie)
		}
		if receivedAuthChecked != "true" {
			t.Fatalf("expected X-Auth-Checked %q, got %q", "true", receivedAuthChecked)
		}
		if receivedUserID != "test-user" {
			t.Fatalf("expected X-User-Id %q, got %q", "test-user", receivedUserID)
		}
		if receivedTenantID != "case-tenant" {
			t.Fatalf("expected X-Tenant-Id %q, got %q", "case-tenant", receivedTenantID)
		}
		if receivedPermissions != "ai.assistant.use,ai.knowledge.read" {
			t.Fatalf("expected X-Permissions %q, got %q", "ai.assistant.use,ai.knowledge.read", receivedPermissions)
		}
		if receivedContentType != "application/json" {
			t.Fatalf("expected Content-Type %q, got %q", "application/json", receivedContentType)
		}
	})

	t.Run("unset cookie omits Cookie header and preserves default behavior", func(t *testing.T) {
		t.Setenv("AI_EVAL_COOKIE", "")

		var hasCookieHeader bool
		var receivedCookie string
		var receivedTenantID string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, hasCookieHeader = r.Header["Cookie"]
			receivedCookie = r.Header.Get("Cookie")
			receivedTenantID = r.Header.Get("X-Tenant-Id")

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(knowledge.QueryResponse{LatencyMs: 5})
		}))
		defer server.Close()

		q := HTTPQuery(server.URL, "test-user", "default-tenant", "ai.assistant.use", "", server.Client())
		_, err := q(context.Background(), Case{Query: "policy query"})
		if err != nil {
			t.Fatalf("unexpected query error: %v", err)
		}

		if hasCookieHeader || receivedCookie != "" {
			t.Fatalf("expected Cookie header to be omitted when unset, got hasCookieHeader=%v, cookie=%q", hasCookieHeader, receivedCookie)
		}
		if receivedTenantID != "default-tenant" {
			t.Fatalf("expected fallback X-Tenant-Id %q, got %q", "default-tenant", receivedTenantID)
		}
	})

	t.Run("env fallback propagates AI_EVAL_COOKIE header", func(t *testing.T) {
		t.Setenv("AI_EVAL_COOKIE", "arda_sid=env_session_67890")

		var receivedCookie string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedCookie = r.Header.Get("Cookie")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(knowledge.QueryResponse{LatencyMs: 6})
		}))
		defer server.Close()

		q := HTTPQuery(server.URL, "test-user", "default-tenant", "ai.assistant.use", "", server.Client())
		_, err := q(context.Background(), Case{Query: "policy query"})
		if err != nil {
			t.Fatalf("unexpected query error: %v", err)
		}

		if receivedCookie != "arda_sid=env_session_67890" {
			t.Fatalf("expected Cookie header %q, got %q", "arda_sid=env_session_67890", receivedCookie)
		}
	})

	t.Run("explicit cookie overrides env fallback", func(t *testing.T) {
		t.Setenv("AI_EVAL_COOKIE", "arda_sid=env_cookie")

		var receivedCookie string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedCookie = r.Header.Get("Cookie")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(knowledge.QueryResponse{LatencyMs: 4})
		}))
		defer server.Close()

		q := HTTPQuery(server.URL, "test-user", "default-tenant", "ai.assistant.use", "arda_sid=explicit_cookie", server.Client())
		_, err := q(context.Background(), Case{Query: "policy query"})
		if err != nil {
			t.Fatalf("unexpected query error: %v", err)
		}

		if receivedCookie != "arda_sid=explicit_cookie" {
			t.Fatalf("expected explicit cookie %q, got %q", "arda_sid=explicit_cookie", receivedCookie)
		}
	})
}
