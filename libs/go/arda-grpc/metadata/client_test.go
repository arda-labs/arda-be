package metadata

import (
	"net/http/httptest"
	"testing"
)

func TestAppendToHTTP_SetsDelegatedSubjectHeaders(t *testing.T) {
	m := Context{
		RequestID:     "req-1",
		TraceID:       "trace-1",
		TenantID:      "tenant-a",
		UserID:        "user-1",
		ActorUserID:   "user-1",
		OrgID:         "org-hcm",
		OrgIDs:        []string{"org-hcm", "org-hn"},
		Roles:         []string{"crm_agent", "finance_viewer"},
		Permissions:   []string{"crm.customer.read", "finance.read"},
		AuthChecked:   "true",
		SourceService: "ai-service",
	}
	req := httptest.NewRequest("GET", "/internal/ai/customers/1", nil)
	AppendToHTTP(req.Header, m)

	if req.Header.Get(TenantID) != "tenant-a" {
		t.Errorf("TenantID = %q", req.Header.Get(TenantID))
	}
	if req.Header.Get(UserID) != "user-1" {
		t.Errorf("UserID = %q", req.Header.Get(UserID))
	}
	if req.Header.Get(OrgID) != "org-hcm" {
		t.Errorf("OrgID = %q", req.Header.Get(OrgID))
	}
	if req.Header.Get(OrgIDs) != "org-hcm,org-hn" {
		t.Errorf("OrgIDs = %q", req.Header.Get(OrgIDs))
	}
	if req.Header.Get(Permissions) != "crm.customer.read,finance.read" {
		t.Errorf("Permissions = %q", req.Header.Get(Permissions))
	}
	if req.Header.Get(AuthChecked) != "true" {
		t.Errorf("AuthChecked = %q", req.Header.Get(AuthChecked))
	}
	if req.Header.Get(RequestID) != "req-1" {
		t.Errorf("RequestID = %q", req.Header.Get(RequestID))
	}
}

func TestAppendToHTTP_SkipsEmptyValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/internal/ai/users", nil)
	AppendToHTTP(req.Header, Context{})
	// No headers at all should be set for an empty context.
	if len(req.Header) != 0 {
		t.Errorf("expected no headers, got %v", req.Header)
	}
}
