package service

import (
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

func testCaseForSubmitVariables() *repository.BusinessCase {
	return &repository.BusinessCase{
		ID:                "case-1",
		TenantID:          "tenant-1",
		CaseType:          "CUSTOMER_REGISTRATION",
		CaseCode:          "CS-001",
		PrimaryObjectType: "CUSTOMER",
		PrimaryObjectID:   "cus-1",
		DomainService:     "crm-service",
		CreatedBy:         "maker-1",
	}
}

func TestBuildCaseVariablesIgnoresClientSystemKeys(t *testing.T) {
	bc := testCaseForSubmitVariables()
	client := map[string]any{
		"tenantId":          "tenant-attacker",
		"caseId":            "case-attacker",
		"caseType":          "FORGED",
		"caseCode":          "FORGED-1",
		"domainService":     "loan-service",
		"primaryObjectType": "LOAN",
		"primaryObjectId":   "loan-attacker",
		"customerId":        "cus-attacker",
		"actorUserId":       "victim-1",
		"actor_user_id":     "victim-1",
		"createdBy":         "victim-1",
		"created_by":        "victim-1",
		"amount":            float64(1500),
		"note":              "ok",
	}

	got := buildCaseVariables(bc, "maker-1", client)

	want := map[string]any{
		"caseId":            "case-1",
		"caseType":          "CUSTOMER_REGISTRATION",
		"caseCode":          "CS-001",
		"tenantId":          "tenant-1",
		"domainService":     "crm-service",
		"primaryObjectType": "CUSTOMER",
		"primaryObjectId":   "cus-1",
		"customerId":        "cus-1",
		"actorUserId":       "maker-1",
		"createdBy":         "maker-1",
	}
	for key, expected := range want {
		if got[key] != expected {
			t.Errorf("%s = %v, want %v", key, got[key], expected)
		}
	}
	if got["amount"] != float64(1500) || got["note"] != "ok" {
		t.Fatalf("non-reserved client variables were dropped: %v", got)
	}
	if client["tenantId"] != "tenant-attacker" || client["caseId"] != "case-attacker" {
		t.Fatalf("buildCaseVariables mutated the client map: %v", client)
	}
}

func TestBuildCaseVariablesWithoutActorKeepsCaseValues(t *testing.T) {
	bc := testCaseForSubmitVariables()
	got := buildCaseVariables(bc, "", map[string]any{"tenantId": "tenant-attacker"})
	if got["tenantId"] != "tenant-1" {
		t.Fatalf("tenantId = %v, want tenant-1", got["tenantId"])
	}
	if _, ok := got["actorUserId"]; ok {
		t.Fatalf("actorUserId must stay absent without an actor: %v", got)
	}
}

func TestDropReservedCaseVariables(t *testing.T) {
	client := map[string]any{
		"tenantId":   "tenant-attacker",
		"created_by": "victim-1",
		"amount":     float64(10),
	}
	dropped := dropReservedCaseVariables(client)
	if len(dropped) != 2 || dropped[0] != "created_by" || dropped[1] != "tenantId" {
		t.Fatalf("dropped = %v, want [created_by tenantId]", dropped)
	}
	if _, ok := client["tenantId"]; ok {
		t.Fatal("tenantId was not stripped from client variables")
	}
	if client["amount"] != float64(10) {
		t.Fatal("non-reserved client variable was stripped")
	}
}

func TestSubmitRequestHashUsesSanitizedVariables(t *testing.T) {
	client := map[string]any{"tenantId": "tenant-attacker", "amount": float64(10)}
	dropped := dropReservedCaseVariables(client)
	if len(dropped) == 0 {
		t.Fatal("expected reserved keys to be dropped")
	}
	spoofed := submitRequestHash("case-1", "maker-1", "idem-1", client)
	clean := submitRequestHash("case-1", "maker-1", "idem-1", map[string]any{"amount": float64(10)})
	if spoofed != clean {
		t.Fatal("idempotency hash must not depend on stripped reserved keys")
	}
}
