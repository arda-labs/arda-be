package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/handler"
	"github.com/arda-labs/arda/apps/ai-service/internal/sandbox"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// TestMintedResultIDRoundTripsThroughWidgetToolAndEndpoint pins the contract
// final review found broken: the id the store really mints (tenant|actor|thread
// plus ":seq", i.e. it contains '|' and ':') must be accepted by renderWidget
// and by GET /api/ai/results/{id}, and must stay namespace-scoped.
func TestMintedResultIDRoundTripsThroughWidgetToolAndEndpoint(t *testing.T) {
	const (
		tenant = "0b5f2c1e-1111-4222-8333-444455556666"
		user   = "9a8b7c6d-1111-4222-8333-444455556666"
		thread = "thread-9_x"
	)
	scope := tools.Context{TenantID: tenant, ActorUserID: user, ExternalThread: thread}
	suite := &CodeModeSuite{ResultStore: sandbox.NewResultStore()}
	id := suite.ResultStore.Put(resultNamespace(scope), json.RawMessage(`{"rows":[1,2,3]}`), nil)
	if id == "" || !strings.Contains(id, "|") || !strings.Contains(id, ":") {
		t.Fatalf("fixture id %q is not the real shape", id)
	}

	// 1) renderWidget accepts the id and echoes it.
	args := `{"title":"t","html":"<p>a</p>","js":"1","result_id":` + mustJSON(t, id) + `,"inputs_summary":"s"}`
	res, err := tools.NewWidgetMetaTool().Execute(context.Background(), scope, json.RawMessage(args))
	if err != nil {
		t.Fatalf("renderWidget rejected a real resultId: %v", err)
	}
	if !strings.Contains(string(res.Data), `"result_id":`+mustJSON(t, id)) {
		t.Fatalf("result_id not echoed: %s", res.Data)
	}

	// 2) The endpoint serves it (path-escaped as the browser sends it).
	router := handler.NewRouterWithOptions(nil, nil, handler.RouterOptions{ResultReader: suite.ReadResult})
	get := func(escapedID, thr, userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/ai/results/"+escapedID+"?thread_id="+url.QueryEscape(thr), nil)
		req.Header.Set("X-Auth-Checked", "true")
		req.Header.Set("X-Tenant-Id", tenant)
		req.Header.Set("X-User-Id", userID)
		req.Header.Set("X-Permissions", "ai.assistant.use")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	for name, escaped := range map[string]string{
		"path-escaped": url.PathEscape(id),
		"raw":          id,
		"%7C encoded":  strings.ReplaceAll(id, "|", "%7C"),
	} {
		rec := get(escaped, thread, user)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"rows":[1,2,3]`) {
			t.Fatalf("%s: status %d body %s", name, rec.Code, rec.Body.String())
		}
	}

	// 3) Another thread / another user cannot read it: 404, never 200.
	if rec := get(url.PathEscape(id), "other-thread", user); rec.Code != http.StatusNotFound {
		t.Fatalf("other thread: status %d", rec.Code)
	}
	if rec := get(url.PathEscape(id), thread, "someone-else"); rec.Code != http.StatusNotFound {
		t.Fatalf("other user: status %d", rec.Code)
	}

	// 4) An encoded slash cannot smuggle a path segment or traversal.
	for _, escaped := range []string{url.PathEscape(id) + "%2F..%2Fx", "..%2Fx", url.PathEscape(id) + "%3Fx"} {
		if rec := get(escaped, thread, user); rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: status %d, want 400", escaped, rec.Code)
		}
	}
}

func mustJSON(t *testing.T, v string) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
