package metadata

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestFromHTTPHeadersPreservesActorTargetAndScope(t *testing.T) {
	headers := http.Header{
		"X-Request-Id":     {"req-1"},
		"X-Trace-Id":       {"trace-1"},
		"X-Tenant-Id":      {"tenant-1"},
		"X-User-Id":        {"actor-1"},
		"X-Target-User-Id": {"target-1"},
		"X-Org-Id":         {"org-2"},
		"X-User-Org-Ids":   {"org-1, org-2"},
		"X-User-Group-Ids": {"group-1"},
		"X-Auth-Risk":      {"high"},
		"X-Auth-Checked":   {"true"},
		"Accept-Language":  {"vi-VN"},
		"Idempotency-Key":  {"idem-1"},
	}

	got := FromHTTPHeaders(headers)
	if got.RequestID != "req-1" || got.TraceID != "trace-1" || got.TenantID != "tenant-1" {
		t.Fatalf("unexpected correlation/scope: %+v", got)
	}
	if got.UserID != "actor-1" || got.TargetUserID != "target-1" || got.OrgID != "org-2" {
		t.Fatalf("unexpected actor/target context: %+v", got)
	}
	if len(got.OrgIDs) != 2 || got.OrgIDs[1] != "org-2" || got.AuthRisk != "high" {
		t.Fatalf("unexpected list/security context: %+v", got)
	}
}

func TestAppendToOutgoingRoundTripsContext(t *testing.T) {
	original := Context{
		RequestID: "req-1", TraceID: "trace-1", TenantID: "tenant-1", UserID: "actor-1",
		ActorUserID: "actor-1", TargetUserID: "target-1", OrgID: "org-1", OrgIDs: []string{"org-1", "org-2"},
		Permissions: []string{"crm.read", "crm.manage"}, AuthChecked: "true",
	}
	ctx := AppendToOutgoing(t.Context(), original)
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected outgoing metadata")
	}
	got := fromMD(md)
	if got.RequestID != original.RequestID || got.TargetUserID != original.TargetUserID || got.OrgID != original.OrgID {
		t.Fatalf("metadata did not round-trip: %+v", got)
	}
	if len(got.OrgIDs) != 2 || len(got.Permissions) != 2 || got.AuthChecked != "true" {
		t.Fatalf("metadata lists did not round-trip: %+v", got)
	}
}

func TestHTTPMiddlewareBindsOutgoingMetadata(t *testing.T) {
	var got Context
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = FromOutgoing(r.Context())
	})
	r := httptest.NewRequest(http.MethodGet, "/api/crm/customers", nil)
	r.Header.Set("X-Request-Id", "req-1")
	r.Header.Set("X-Tenant-Id", "tenant-1")
	r.Header.Set("X-User-Id", "actor-1")
	HTTPMiddleware(next).ServeHTTP(httptest.NewRecorder(), r)
	if got.RequestID != "req-1" || got.TenantID != "tenant-1" || got.UserID != "actor-1" {
		t.Fatalf("middleware did not bind context: %+v", got)
	}
}
