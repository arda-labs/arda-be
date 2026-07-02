package repository

import "testing"

func TestValidateCreateRequiresBusinessReference(t *testing.T) {
	err := validateCreate(CaseCreate{
		TenantID:          "tenant-1",
		CaseType:          "FINANCE_INCOMING_TRANSACTION",
		Title:             "Incoming transaction",
		PrimaryObjectType: "finance_transaction",
		PrimaryObjectID:   "txn-1",
		DomainService:     "finance-service",
		CreatedBy:         "user-1",
	})
	if err != nil {
		t.Fatalf("validateCreate returned error: %v", err)
	}

	err = validateCreate(CaseCreate{TenantID: "tenant-1"})
	if err == nil {
		t.Fatal("validateCreate accepted incomplete case")
	}
}

func TestValidateCaseTypeUpsert(t *testing.T) {
	valid := CaseTypeUpsert{
		CaseType:      "FINANCE_INCOMING_TRANSACTION",
		BusinessArea:  "FINANCE",
		OperationName: "Incoming transaction",
		BpmnProcessID: "finance-incoming-transaction-v1",
		MakerRole:     "FINANCE_TXN_MAKER",
		CheckerRole:   "FINANCE_TXN_CHECKER",
		OwnerService:  "finance-service",
	}
	if err := validateCaseTypeUpsert(valid, true); err != nil {
		t.Fatalf("validateCaseTypeUpsert returned error: %v", err)
	}

	valid.CaseType = ""
	if err := validateCaseTypeUpsert(valid, true); err == nil {
		t.Fatal("validateCaseTypeUpsert accepted missing caseType on create")
	}
	if err := validateCaseTypeUpsert(valid, false); err != nil {
		t.Fatalf("validateCaseTypeUpsert rejected missing caseType on update: %v", err)
	}
}

func TestNewID(t *testing.T) {
	id, err := newID()
	if err != nil {
		t.Fatalf("newID returned error: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("newID length = %d, want 32", len(id))
	}
}
