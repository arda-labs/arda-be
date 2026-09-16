package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/iam-service/internal/hydra"
)

func TestOAuthClientReadEndpointsRedactSecrets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/clients":
			fmt.Fprint(w, `{"items":[{"client_id":"client-1","client_name":"App","client_secret":"hydra-secret","registration_access_token":"registration-token","registration_client_uri":"https://hydra.example/admin/clients/client-1"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/admin/clients/client-1":
			fmt.Fprint(w, `{"client_id":"client-1","client_name":"App","client_secret":"hydra-secret","registration_access_token":"registration-token","registration_client_uri":"https://hydra.example/register?token=registration-token"}`)
		case r.Method == http.MethodPut && r.URL.Path == "/admin/clients/client-1":
			fmt.Fprint(w, `{"client_id":"client-1","client_name":"App","client_secret":"hydra-secret","registration_access_token":"registration-token"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	h := NewOAuthClientHandler(hydra.New(upstream.URL))

	assertNoSecrets := func(t *testing.T, body string) {
		t.Helper()
		for _, forbidden := range []string{"hydra-secret", "registration-token", "client_secret", "registration_access_token"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("response leaks %q: %s", forbidden, body)
			}
		}
		if !strings.Contains(body, "client-1") || !strings.Contains(body, "App") {
			t.Fatalf("response lost non-sensitive fields: %s", body)
		}
	}

	t.Run("list", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Clients(rec, httptest.NewRequest(http.MethodGet, "/api/admin/oauth-clients", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		assertNoSecrets(t, rec.Body.String())
		if !strings.Contains(rec.Body.String(), "registration_client_uri") {
			t.Fatalf("token-free registration_client_uri should be preserved: %s", rec.Body.String())
		}
	})

	t.Run("get", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/oauth-clients/client-1", nil)
		req.SetPathValue("id", "client-1")
		rec := httptest.NewRecorder()
		h.ClientByID(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		assertNoSecrets(t, rec.Body.String())
		if strings.Contains(rec.Body.String(), "registration_client_uri") {
			t.Fatalf("token-bearing registration_client_uri must be stripped: %s", rec.Body.String())
		}
	})

	t.Run("update", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/oauth-clients/client-1",
			strings.NewReader(`{"client_name":"App","redirect_uris":["https://app.example/cb"]}`))
		req.SetPathValue("id", "client-1")
		rec := httptest.NewRecorder()
		h.ClientByID(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		assertNoSecrets(t, rec.Body.String())
	})
}

func TestOAuthClientCreateStillReturnsSecret(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"client_id":"client-2","client_secret":"one-time-secret"}`)
	}))
	defer upstream.Close()

	h := NewOAuthClientHandler(hydra.New(upstream.URL))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/oauth-clients",
		strings.NewReader(`{"client_name":"App","redirect_uris":["https://app.example/cb"]}`))
	rec := httptest.NewRecorder()
	h.Clients(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "one-time-secret") {
		t.Fatalf("create response must expose the secret once: %s", rec.Body.String())
	}
}

func TestRedactClientRemovesOnlySensitiveFields(t *testing.T) {
	redacted := redactClient(map[string]any{
		"client_id":                 "client-1",
		"client_secret":             "secret",
		"registration_access_token": "token",
		"registration_client_uri":   "https://hydra.example/register?token=token",
		"redirect_uris":             []any{"https://app.example/cb"},
	})
	if _, ok := redacted["client_secret"]; ok {
		t.Fatal("client_secret must be removed")
	}
	if _, ok := redacted["registration_access_token"]; ok {
		t.Fatal("registration_access_token must be removed")
	}
	if _, ok := redacted["registration_client_uri"]; ok {
		t.Fatal("token-bearing registration_client_uri must be removed")
	}
	if redacted["client_id"] != "client-1" {
		t.Fatalf("client_id = %v, want client-1", redacted["client_id"])
	}
	if _, ok := redacted["redirect_uris"]; !ok {
		t.Fatal("redirect_uris must be preserved")
	}

	opaque := redactClient(map[string]any{"registration_client_uri": "https://hydra.example/admin/clients/client-1"})
	if _, ok := opaque["registration_client_uri"]; !ok {
		t.Fatal("token-free registration_client_uri must be preserved")
	}

	if redactClient(nil) != nil {
		t.Fatal("nil client must stay nil")
	}
}
