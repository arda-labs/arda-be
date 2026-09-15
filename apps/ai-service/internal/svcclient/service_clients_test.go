package svcclient

import "testing"

func TestNewServiceClientsBuildsOneClientPerConfiguredURL(t *testing.T) {
	clients := NewServiceClients(map[string]string{
		"crm-service":      "http://crm.local/",
		"workflow-service": "http://workflow.local",
		"mdm-service":      "",
	}, "ai-service", "01234567890123456789012345678901", nil)

	if len(clients) != 2 {
		t.Fatalf("clients = %d, want 2 (empty URL must be omitted)", len(clients))
	}
	crm, ok := clients["crm-service"]
	if !ok {
		t.Fatal("crm-service client missing")
	}
	if crm.ServiceName != "crm-service" || crm.BaseURL != "http://crm.local" {
		t.Fatalf("crm client = %+v, want service name crm-service and trimmed base URL", crm)
	}
	if crm.Source != "ai-service" || crm.Secret != "01234567890123456789012345678901" {
		t.Fatal("caller identity must be propagated to every service client")
	}
	if _, present := clients["mdm-service"]; present {
		t.Fatal("empty base URL must not produce a client")
	}
}
