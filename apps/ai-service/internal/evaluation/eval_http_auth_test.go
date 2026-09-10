package evaluation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

func TestHTTPQuerySignsWorkloadIdentityWhenConfigured(t *testing.T) {
	secret := strings.Repeat("s3cr3t", 8)
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(identity.MetadataKey)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"r1","hits":[],"latency_ms":1,"retrieved_count":0,"reranked_count":0}`))
	}))
	defer server.Close()

	t.Setenv("AI_EVAL_SERVICE_SECRET", secret)
	query := HTTPQuery(server.URL, "ai-eval", "tenant-1", "ai.assistant.use", "", server.Client())
	if _, err := query(context.Background(), Case{ID: "c1", Tenant: "tenant-1", Query: "nghỉ phép"}); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if gotToken == "" {
		t.Fatal("expected x-service-auth header when AI_EVAL_SERVICE_SECRET is set")
	}
	claims, err := identity.Verify(gotToken, secret, "ai-service", time.Now())
	if err != nil {
		t.Fatalf("signed token did not verify: %v", err)
	}
	if claims.Source != "auth-gateway" {
		t.Fatalf("expected source auth-gateway, got %q", claims.Source)
	}
}

func TestHTTPQueryUnsignedWithoutSecret(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(identity.MetadataKey)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"r1","hits":[],"latency_ms":1,"retrieved_count":0,"reranked_count":0}`))
	}))
	defer server.Close()

	t.Setenv("AI_EVAL_SERVICE_SECRET", "")
	query := HTTPQuery(server.URL, "ai-eval", "tenant-1", "ai.assistant.use", "", server.Client())
	if _, err := query(context.Background(), Case{ID: "c1", Tenant: "tenant-1", Query: "nghỉ phép"}); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if gotToken != "" {
		t.Fatalf("expected no x-service-auth header, got %q", gotToken)
	}
}
