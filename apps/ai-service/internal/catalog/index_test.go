package catalog

import (
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func TestCatalogIndex_SearchAndPermissions(t *testing.T) {
	entries := []CatalogEntry{
		{
			MethodName: "crm.getCustomer",
			SDKPath:    "arda.crm.getCustomer",
			Domain:     "crm",
			Signature:  "arda.crm.getCustomer(args: { customerId: string }): Promise<CustomerSummary>;",
			JSDoc:      "/** Read customer summary */",
			Keywords:   []string{"customer", "crm", "client", "get"},
			RequiredPermissions: []string{"crm.customer.read"},
		},
		{
			MethodName: "crm.exportCustomer",
			SDKPath:    "arda.crm.exportCustomer",
			Domain:     "crm",
			Signature:  "arda.crm.exportCustomer(args: { customerId: string }): Promise<ApprovalProposal>;",
			JSDoc:      "/** Export customer */",
			Keywords:   []string{"export", "customer", "download"},
			RequiredPermissions: []string{"crm.customer.export"},
		},
		{
			MethodName: "knowledge.search",
			SDKPath:    "arda.knowledge.search",
			Domain:     "knowledge",
			Signature:  "arda.knowledge.search(args: { query: string }): Promise<KnowledgeSearchResult[]>;",
			JSDoc:      "/** Search knowledge documentation */",
			Keywords:   []string{"knowledge", "search", "faq", "doc"},
			RequiredPermissions: []string{"ai.knowledge.read"},
		},
	}

	index := NewIndex(entries)

	// User has all permissions
	fullScope := tools.Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
		Permissions: map[string]struct{}{
			"crm.customer.read":   {},
			"crm.customer.export": {},
			"ai.knowledge.read":   {},
		},
	}

	// 1. Search for customer
	hits := index.Search("customer details", "", fullScope, 5)
	if len(hits) < 1 {
		t.Fatalf("expected at least 1 hit, got %d", len(hits))
	}
	if hits[0].MethodName != "crm.getCustomer" && hits[0].MethodName != "crm.exportCustomer" {
		t.Errorf("unexpected top hit: %s", hits[0].MethodName)
	}

	// 2. Domain filter
	knowledgeHits := index.Search("search", "knowledge", fullScope, 5)
	if len(knowledgeHits) != 1 || knowledgeHits[0].MethodName != "knowledge.search" {
		t.Errorf("expected knowledge.search only, got %v", knowledgeHits)
	}

	// 3. Permission masking: user without crm.customer.export should not see exportCustomer
	readOnlyScope := tools.Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
		Permissions: map[string]struct{}{
			"crm.customer.read": {},
		},
	}

	restrictedHits := index.Search("export customer", "", readOnlyScope, 5)
	for _, hit := range restrictedHits {
		if hit.MethodName == "crm.exportCustomer" {
			t.Errorf("expected crm.exportCustomer to be filtered out for read-only user, but found in hits")
		}
	}

	// 4. Format signatures
	sigText := FormatSignatures(hits)
	if !strings.Contains(sigText, "arda.crm.") {
		t.Errorf("expected formatted signatures to contain arda.crm., got:\n%s", sigText)
	}
}
