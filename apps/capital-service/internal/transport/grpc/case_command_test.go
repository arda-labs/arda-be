package grpc

import (
	"context"
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/capital-service/internal/migration"
	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	"github.com/arda-labs/arda/apps/capital-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const caseTestTenant = "tenant-case-command"

func tenantContext(tenantID string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(ardametadata.TenantID, tenantID))
}

// newCaseCommandFixture builds the adapter on a migrated capital database
// without finance or workflow clients: contract and amendment decisions
// need neither, and movement approval (which posts) is covered only on its
// idempotent POSTED path.
func newCaseCommandFixture(t *testing.T) (*CaseCommandServer, *sql.DB) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	svc := service.NewCapitalService(repository.NewCapitalRepository(db), db, nil, nil)
	return NewCaseCommandServer(svc), db
}

func insertContract(t *testing.T, db *sql.DB, code, contractStatus string) string {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO cfc_fund_types (id, tenant_id, code, name) VALUES ('ft-test', $1, 'FT', 'Fund type')
		ON CONFLICT (id) DO NOTHING`, caseTestTenant); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRow(`
		INSERT INTO cfc_contracts (tenant_id, contract_code, fund_type_code, counterparty_code, contract_date, amount_minor, interest_rate, status, created_by)
		VALUES ($1, $2, 'FT', 'CP-1', CURRENT_DATE, 1000000, 5.5, $3, 'tester') RETURNING id::text`,
		caseTestTenant, code, contractStatus).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertAmendment(t *testing.T, db *sql.DB, contractID, amendmentStatus string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO cfc_contract_amendments (tenant_id, contract_id, status, payload, created_by)
		VALUES ($1, $2::uuid, $3, '{"amount_minor": 2500000}', 'tester') RETURNING id::text`,
		caseTestTenant, contractID, amendmentStatus).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMovement(t *testing.T, db *sql.DB, contractID, movementType, movementStatus string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO cfc_movements (tenant_id, contract_id, movement_type, amount_minor, movement_date, status, created_by)
		VALUES ($1, $2::uuid, $3, 300000, CURRENT_DATE, $4, 'tester') RETURNING id::text`,
		caseTestTenant, contractID, movementType, movementStatus).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func rowStatus(t *testing.T, db *sql.DB, table, id string) string {
	t.Helper()
	var current string
	if err := db.QueryRow(`SELECT status FROM `+table+` WHERE id = $1::uuid`, id).Scan(&current); err != nil {
		t.Fatal(err)
	}
	return current
}

func resolve(ctx context.Context, server *CaseCommandServer, kind, objectID string, decision commonv1.CaseDecision, dataVersion string) (*commonv1.ResolveCaseResponse, error) {
	return server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{
		Kind: kind, ObjectId: objectID, Decision: decision, DecidedBy: "checker-1", DataVersion: dataVersion,
	})
}

func TestCaseCommandContractCheckAndApproveIsIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	pending := insertContract(t, db, "HD-1", "PENDING_APPROVAL")
	active := insertContract(t, db, "HD-2", "ACTIVE")
	ctx := tenantContext(caseTestTenant)

	resp, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindContract, ObjectId: pending})
	if err != nil || !resp.GetOk() {
		t.Fatalf("pending contract: %v %v", resp, err)
	}
	resp, err = server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindContract, ObjectId: active})
	if err != nil || resp.GetOk() || resp.GetMessage() == "" {
		t.Fatalf("active contract must fail the check with a message: %v %v", resp, err)
	}
	resp, err = server.CheckCase(tenantContext("other-tenant"), &commonv1.CheckCaseRequest{Kind: KindContract, ObjectId: pending})
	if err != nil || resp.GetOk() {
		t.Fatalf("another tenant must not see the contract: %v %v", resp, err)
	}
	for i := 0; i < 2; i++ {
		out, err := resolve(ctx, server, KindContract, pending, commonv1.CaseDecision_CASE_DECISION_APPROVE, "1")
		if err != nil || out.GetOutputs()["capitalDecision"] != "APPROVE" {
			t.Fatalf("approve #%d: %v %v", i+1, out, err)
		}
	}
	if got := rowStatus(t, db, "cfc_contracts", pending); got != "ACTIVE" {
		t.Fatalf("contract status = %s", got)
	}
	if _, err := resolve(ctx, server, KindContract, pending, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an active contract = %v, want FailedPrecondition", err)
	}
}

func TestCaseCommandContractRejectIsIdempotentAndGuarded(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	pending := insertContract(t, db, "HD-R", "PENDING_APPROVAL")
	stale := insertContract(t, db, "HD-S", "PENDING_APPROVAL")
	ctx := tenantContext(caseTestTenant)

	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindContract, pending, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "cfc_contracts", pending); got != "REJECTED" {
		t.Fatalf("contract status = %s", got)
	}
	if _, err := resolve(ctx, server, KindContract, pending, commonv1.CaseDecision_CASE_DECISION_APPROVE, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected contract = %v, want FailedPrecondition", err)
	}
	if _, err := resolve(ctx, server, KindContract, stale, commonv1.CaseDecision_CASE_DECISION_APPROVE, "9"); status.Code(err) != codes.Aborted {
		t.Fatalf("stale data version = %v, want Aborted", err)
	}
	if _, err := resolve(tenantContext("other-tenant"), server, KindContract, stale, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("another tenant = %v, want NotFound", err)
	}
}

func TestCaseCommandAmendmentAppliesPayloadOnce(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	contractID := insertContract(t, db, "HD-A", "ACTIVE")
	amendment := insertAmendment(t, db, contractID, "SUBMITTED")
	rejected := insertAmendment(t, db, contractID, "SUBMITTED")
	ctx := tenantContext(caseTestTenant)

	resp, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindAmendment, ObjectId: amendment})
	if err != nil || !resp.GetOk() {
		t.Fatalf("submitted amendment: %v %v", resp, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindAmendment, amendment, commonv1.CaseDecision_CASE_DECISION_APPROVE, ""); err != nil {
			t.Fatalf("approve #%d: %v", i+1, err)
		}
	}
	var amount int64
	if err := db.QueryRow(`SELECT amount_minor FROM cfc_contracts WHERE id = $1::uuid`, contractID).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 2500000 || rowStatus(t, db, "cfc_contract_amendments", amendment) != "APPLIED" {
		t.Fatalf("amount=%d status=%s, want the payload applied once and APPLIED", amount, rowStatus(t, db, "cfc_contract_amendments", amendment))
	}
	if _, err := resolve(ctx, server, KindAmendment, amendment, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an applied amendment = %v, want FailedPrecondition", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindAmendment, rejected, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "cfc_contract_amendments", rejected); got != "REJECTED" {
		t.Fatalf("amendment status = %s", got)
	}
}

func TestCaseCommandMovementRejectAndPostedApproveAreIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	contractID := insertContract(t, db, "HD-M", "ACTIVE")
	submitted := insertMovement(t, db, contractID, "RECEIPT", "SUBMITTED")
	posted := insertMovement(t, db, contractID, "RECEIPT", "POSTED")
	ctx := tenantContext(caseTestTenant)

	resp, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindMovement, ObjectId: submitted})
	if err != nil || !resp.GetOk() {
		t.Fatalf("submitted movement: %v %v", resp, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindMovement, submitted, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "cfc_movements", submitted); got != "REJECTED" {
		t.Fatalf("movement status = %s", got)
	}
	if _, err := resolve(ctx, server, KindMovement, submitted, commonv1.CaseDecision_CASE_DECISION_APPROVE, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected movement = %v, want FailedPrecondition", err)
	}
	// A movement already POSTED is the retried approval: no second posting.
	if _, err := resolve(ctx, server, KindMovement, posted, commonv1.CaseDecision_CASE_DECISION_APPROVE, ""); err != nil {
		t.Fatalf("retried approve of a posted movement: %v", err)
	}
	if _, err := resolve(ctx, server, KindMovement, posted, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting a posted movement = %v, want FailedPrecondition", err)
	}
}

func TestCaseCommandRejectsUnknownKindAndInput(t *testing.T) {
	server := NewCaseCommandServer(service.NewCapitalService(nil, nil, nil, nil))
	ctx := tenantContext(caseTestTenant)
	if _, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: "dpm.settle", ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown kind = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindContract, ObjectId: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty object = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindContract, ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified decision = %v", err)
	}
	if _, err := server.ResolveCase(context.Background(), &commonv1.ResolveCaseRequest{Kind: KindContract, ObjectId: "x", Decision: commonv1.CaseDecision_CASE_DECISION_APPROVE}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("missing tenant = %v", err)
	}
}
