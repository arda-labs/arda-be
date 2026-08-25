package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCRMCustomerGetToolScopesRequestAndRedactsCustomerFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/crm/customers/customer-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Tenant-Id") != "tenant-1" || r.Header.Get("X-User-Org-Ids") != "org-1" || r.Header.Get("X-Org-Id") != "org-1" {
			t.Fatalf("scope headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"id":"customer-1","customerCode":"C-1","name":"A Customer","customerType":"PERSONAL","status":"ACTIVE","email":"private@example.test","identityNo":"secret","orgId":"org-1"}}`))
	}))
	defer server.Close()

	tool := NewCRMCustomerGetTool(server.URL, server.Client())
	result, err := tool.Execute(context.Background(), Context{
		TenantID: "tenant-1", ActorUserID: "user-1", OrgIDs: []string{"org-1"}, ActiveOrgID: "org-1",
		Permissions: map[string]struct{}{"crm.customer.read": {}},
	}, json.RawMessage(`{"customerId":"customer-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "Customer A Customer (C-1) is ACTIVE." {
		t.Fatalf("summary = %q", result.Summary)
	}
	if strings.Contains(string(result.Data), "private@example.test") || strings.Contains(string(result.Data), "identityNo") {
		t.Fatalf("sensitive fields leaked: %s", result.Data)
	}
}

func TestCRMCustomerGetToolRejectsUnknownArguments(t *testing.T) {
	tool := NewCRMCustomerGetTool("http://crm", nil)
	_, err := tool.Execute(context.Background(), Context{TenantID: "tenant", ActorUserID: "user"}, json.RawMessage(`{"customerId":"customer-1","email":"forbidden"}`))
	if err == nil || !strings.Contains(err.Error(), ErrInvalidArgument.Error()) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}
