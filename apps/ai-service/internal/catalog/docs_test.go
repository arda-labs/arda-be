package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeDocs implements docsLookuper for unit tests. It captures the requested
// code so callers can assert dispatch.
type fakeDocs struct {
	capturedCode string
}

func (f *fakeDocs) Lookup(ctx context.Context, code string) (any, error) {
	f.capturedCode = code
	return map[string]any{
		"found": true,
		"page": map[string]any{
			"code":    code,
			"title":   "Recent auth required",
			"status":  403,
			"summary": "step-up required",
			"url":     "https://docs.arda.io.vn/problems/" + code + "/",
		},
	}, nil
}

// TestDocsProblemLookupShape guards the Goja contract: the dispatcher returns
// the { found, page } shape promised by the JSDoc and passes the code through
// unchanged.
func TestDocsProblemLookupShape(t *testing.T) {
	reg := NewDispatcherRegistry()
	fake := &fakeDocs{}
	RegisterDocsCatalog(reg, fake)

	fn, entry, ok := reg.Resolve("docs.problemLookup")
	if !ok {
		t.Fatal("docs.problemLookup not registered")
	}

	scope := iamScope()
	// RequiredPermissions is nil: any authenticated actor may look up the
	// public catalog.
	if err := entry.CheckPermissions(scope); err != nil {
		t.Fatalf("expected permission granted without extra permissions, got %v", err)
	}
	if entry.Kind != "read" {
		t.Fatalf("expected kind read, got %s", entry.Kind)
	}

	result, err := fn(context.Background(), scope, map[string]any{"code": "recent_auth_required"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.capturedCode != "recent_auth_required" {
		t.Fatalf("expected code passthrough, got %q", fake.capturedCode)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var shaped map[string]any
	if err := json.Unmarshal(raw, &shaped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if shaped["found"] != true {
		t.Fatalf("expected found=true, got %v", shaped["found"])
	}
	page, ok := shaped["page"].(map[string]any)
	if !ok {
		t.Fatalf("expected page object, got %T", shaped["page"])
	}
	for _, key := range []string{"code", "title", "status", "summary", "url"} {
		if _, present := page[key]; !present {
			t.Errorf("contract field %q missing from page (got keys %v)", key, page)
		}
	}
}

func TestDocsProblemLookupValidation(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterDocsCatalog(reg, &fakeDocs{})

	fn, _, ok := reg.Resolve("docs.problemLookup")
	if !ok {
		t.Fatal("docs.problemLookup not registered")
	}

	if _, err := fn(context.Background(), iamScope(), map[string]any{}); err == nil {
		t.Fatal("expected error for missing code")
	}
	if _, err := fn(context.Background(), iamScope(), map[string]any{"code": ""}); err == nil {
		t.Fatal("expected error for empty code")
	}
}

// TestHTTPDocsLookuperIntegration runs the real HTTP client against a stub
// server to verify request/response plumbing.
func TestHTTPDocsLookuperIntegration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("code") != "validation.invalid_input" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"found":false,"suggestions":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"found":true,"page":{"code":"validation.invalid_input","title":"Request is invalid","status":400,"summary":"bad payload","url":"https://docs.arda.io.vn/problems/validation.invalid_input/"}}`))
	}))
	defer srv.Close()

	lookup := NewHTTPDocsLookuper(srv.URL)
	result, err := lookup.Lookup(context.Background(), "validation.invalid_input")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", result)
	}
	if payload["found"] != true {
		t.Fatalf("expected found=true, got %v", payload["found"])
	}

	// A 404 from the lookup API still carries a parseable body
	// ({found:false, suggestions}) — the model needs the suggestions, so the
	// client must not turn non-2xx into a transport error.
	unknown, err := lookup.Lookup(context.Background(), "unknown.code")
	if err != nil {
		t.Fatalf("unknown code should not error: %v", err)
	}
	unknownMap, ok := unknown.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", unknown)
	}
	if unknownMap["found"] != false {
		t.Fatalf("expected found=false for unknown code, got %v", unknownMap["found"])
	}
}
