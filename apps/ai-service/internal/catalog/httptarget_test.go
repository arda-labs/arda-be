package catalog

import (
	"reflect"
	"testing"
)

// TestRegisterGeneratedCatalogReportsUnwiredServices locks the fail-loud
// contract: a service that has generated entries but no configured base URL is
// reported to the caller (production refuses to start on a non-empty list)
// instead of silently dropping its tools.
func TestRegisterGeneratedCatalogReportsUnwiredServices(t *testing.T) {
	reg := NewDispatcherRegistry()
	skipped := RegisterGeneratedCatalog(reg, ClientSet{
		"crm-service": testClient("crm-service", "http://crm.local"),
	})

	want := []string{
		"capital-service", "deposit-service", "finance-service", "hrm-service",
		"iam-service", "loan-service", "mdm-service", "notification-service",
		"platform-service", "statistical-service", "workflow-service",
	}
	if !reflect.DeepEqual(skipped, want) {
		t.Fatalf("skipped services = %v, want %v", skipped, want)
	}

	fn, entry, ok := reg.Resolve("crm.getCustomer")
	if !ok || fn == nil {
		t.Fatal("wired crm.getCustomer must register")
	}
	if entry.Service != "crm-service" {
		t.Fatalf("entry.Service = %q, want crm-service", entry.Service)
	}
	if _, _, ok := reg.Resolve("iam.listUsers"); ok {
		t.Fatal("unwired iam.listUsers must not register")
	}
}
