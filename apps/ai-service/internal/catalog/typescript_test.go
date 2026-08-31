package catalog

import (
	"strings"
	"testing"
)

func TestGenerateTypeDefinitions_Empty(t *testing.T) {
	out := GenerateTypeDefinitions(nil)
	if !strings.Contains(out, "No arda.* SDK methods") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestGenerateTypeDefinitions_ContainsNamespace(t *testing.T) {
	entries := []CatalogEntry{
		{
			MethodName: "crm.getCustomer",
			SDKPath:    "arda.crm.getCustomer",
			Domain:     "crm",
			Signature:  "arda.crm.getCustomer(args: { customerId: string }): Promise<CustomerSummary>",
			JSDoc:      "/**\n * Read a redacted customer summary.\n * @param args.customerId ...\n */",
			Keywords:   []string{"customer", "get"},
		},
		{
			MethodName: "finance.getAccount",
			SDKPath:    "arda.finance.getAccount",
			Domain:     "finance",
			Signature:  "arda.finance.getAccount(args: { accountId: string }): Promise<Account>",
			JSDoc:      "/** Read a chart-of-accounts entry. */",
			Keywords:   []string{"finance", "account"},
		},
	}
	out := GenerateTypeDefinitions(entries)

	t.Logf("Generated .d.ts:\n%s", out)

	if !strings.Contains(out, "declare namespace arda") {
		t.Error("missing top-level namespace")
	}
	if !strings.Contains(out, "namespace crm") {
		t.Error("missing crm namespace")
	}
	if !strings.Contains(out, "namespace finance") {
		t.Error("missing finance namespace")
	}
	if !strings.Contains(out, "getCustomer") {
		t.Error("missing getCustomer declaration")
	}
	if !strings.Contains(out, "getAccount") {
		t.Error("missing getAccount declaration")
	}
	if !strings.Contains(out, "Read a redacted customer summary") {
		t.Error("missing JSDoc summary line")
	}
	if strings.Contains(out, "arda.crm.getCustomer") {
		t.Error("signature should strip arda.crm prefix")
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "}") {
		t.Error("should end with closing brace")
	}
}

func TestGenerateTypeDefinitions_NoDuplicate(t *testing.T) {
	entries := []CatalogEntry{
		{MethodName: "crm.getCustomer", SDKPath: "arda.crm.getCustomer", Domain: "crm", Signature: "arda.crm.getCustomer(a: string): R"},
		{MethodName: "crm.getCustomer", SDKPath: "arda.crm.getCustomer", Domain: "crm", Signature: "arda.crm.getCustomer(a: string): R"},
	}
	out := GenerateTypeDefinitions(entries)
	// Should contain the method exactly once (Sort and dedup by domain grouping).
	n := strings.Count(out, "getCustomer")
	if n != 1 {
		t.Errorf("expected getCustomer once, got %d", n)
	}
}
