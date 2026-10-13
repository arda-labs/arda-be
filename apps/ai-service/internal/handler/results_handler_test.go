package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	}
	return req
}

func TestReadResultRequiresIdentity(t *testing.T) {
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, false }).
		ServeHTTP(rec, resultsRequest("res1", "th1", false))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestReadResultRequiresIdentityHeadersAfterAuthCheck(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ai/results/res1?thread_id=th1", nil)
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
		ServeHTTP(rec, resultsRequest("res1", "", true))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestReadResultRejectsNonGet(t *testing.T) {
	req := resultsRequest("res1", "th1", true)
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
	router.ServeHTTP(rec, resultsRequest("res_1", "thread-9", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	if got.TenantID != "t1" || got.ActorUserID != "u1" || got.ExternalThread != "thread-9" || gotID != "res_1" {
		t.Fatalf("scope not forwarded: %+v id=%s", got, gotID)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || string(body["output"]) != `{"rows":[1,2]}` {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestReadResultNotFoundHidesExistence(t *testing.T) {
	// Review Focus 4: another thread's id must look like a missing id.
	rec := httptest.NewRecorder()
	newResultsRouter(func(tools.Context, string) (json.RawMessage, bool) { return nil, false }).
		ServeHTTP(rec, resultsRequest("res_1", "other", true))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReadResultNilReaderIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	newResultsRouter(nil).ServeHTTP(rec, resultsRequest("res_1", "th", true))
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
