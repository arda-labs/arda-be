package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func newResultsRouter(reader func(scope tools.Context, resultID string) (json.RawMessage, bool)) http.Handler {
	return newRouter(nil, nil, RouterOptions{ResultReader: reader})
}

func resultsRequest(id, thread string, withIdentity bool) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/ai/results/"+id+"?thread_id="+thread, nil)
	if withIdentity {
		req.Header.Set("X-Auth-Checked", "true")
		req.Header.Set("X-Tenant-Id", "t1")
		req.Header.Set("X-User-Id", "u1")
		req.Header.Set("X-Permissions", assistantPermission)
	}
	return req
}

func TestReadResultRequiresIdentity(t *testing.T) {
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, false }).
		ServeHTTP(rec, resultsRequest("t1%7Cu1%7Cth1:1", "th1", false))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestReadResultRequiresIdentityHeadersAfterAuthCheck(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ai/results/t1%7Cu1%7Cth1:1?thread_id=th1", nil)
	req.Header.Set("X-Auth-Checked", "true")
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, true }).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestReadResultRequiresThread(t *testing.T) {
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, false }).
		ServeHTTP(rec, resultsRequest("t1%7Cu1%7Cth1:1", "", true))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestReadResultRejectsNonGet(t *testing.T) {
	req := resultsRequest("t1%7Cu1%7Cth1:1", "th1", true)
	req.Method = http.MethodPost
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, true }).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestReadResultScopesByTenantUserThread(t *testing.T) {
	var got tools.Context
	var gotID string
	router := newResultsRouter(func(scope tools.Context, id string) (json.RawMessage, bool) {
		got, gotID = scope, id
		return json.RawMessage(`{"rows":[1,2]}`), true
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resultsRequest("t1%7Cu1%7Cth-9:1", "thread-9", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	if got.TenantID != "t1" || got.ActorUserID != "u1" || got.ExternalThread != "thread-9" || gotID != "t1|u1|th-9:1" {
		t.Fatalf("scope not forwarded: %+v id=%s", got, gotID)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store (tenant data in a GET)", cc)
	}
	// Canonical success envelope, same as the sibling AI handlers.
	var body struct {
		Success  bool              `json:"success"`
		Errors   []json.RawMessage `json:"errors"`
		Messages []json.RawMessage `json:"messages"`
		Result   struct {
			Output json.RawMessage `json:"output"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %s: %v", rec.Body.String(), err)
	}
	if !body.Success || string(body.Result.Output) != `{"rows":[1,2]}` {
		t.Fatalf("envelope = %s", rec.Body.String())
	}
	if body.Errors == nil || len(body.Errors) != 0 || body.Messages == nil || len(body.Messages) != 0 {
		t.Fatalf("errors/messages must be empty arrays, got %s", rec.Body.String())
	}
}

func TestReadResultNotFoundHidesExistence(t *testing.T) {
	// Review Focus 4: another thread's id must look like a missing id.
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, false }).
		ServeHTTP(rec, resultsRequest("t1%7Cu1%7Cth-9:1", "other", true))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReadResultNilReaderIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	newResultsRouter(nil).ServeHTTP(rec, resultsRequest("t1%7Cu1%7Cth-9:1", "th", true))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReadResultRejectsBadID(t *testing.T) {
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, true }).
		ServeHTTP(rec, resultsRequest("..%2Fx", "th", true))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestReadResultRejectsMalformedIDs(t *testing.T) {
	called := false
	router := newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { called = true; return nil, true })
	for _, id := range []string{
		"t1%7Cu1%7Cth1",           // no :seq suffix
		"t1%7Cu1%7Cth1:zz",        // non-hex seq
		"t1%7Cu1%7Ca%2Fb:1",       // encoded slash
		"t1%7Cu1%7Cth1:1%3Fx%3D1", // encoded query
		"t1%7Cu1%7Cth1:1%23x",     // encoded fragment
		"t1%7Cu1%7Ca%25b:1",       // encoded percent
		"t1%7Cu1%7Ca%20b:1",       // space
		"t1%7Cu1%7Ca%01b:1",       // control character
		"t1%7Cu1%7Ca%22b:1",       // quote
		"t1%7Cu1%7Ca%5Cb:1",       // backslash
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, resultsRequest(id, "th1", true))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", id, rec.Code)
		}
	}
	if called {
		t.Fatal("reader must not be consulted for a malformed id")
	}
}

func TestReadResultRequiresAssistantPermission(t *testing.T) {
	called := false
	router := newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { called = true; return json.RawMessage(`{}`), true })
	for name, perms := range map[string]string{"absent": "", "other permission": "ai.settings.read", "prefix only": "ai.assistant"} {
		req := resultsRequest("t1%7Cu1%7Cth1:1", "th1", true)
		if perms == "" {
			req.Header.Del("X-Permissions")
		} else {
			req.Header.Set("X-Permissions", perms)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "ai.assistant_forbidden") {
			t.Fatalf("%s: status = %d body %s, want 403 ai.assistant_forbidden", name, rec.Code, rec.Body.String())
		}
	}
	if called {
		t.Fatal("reader must not be consulted without ai.assistant.use")
	}
}
