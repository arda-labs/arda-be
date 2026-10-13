package grpc

import (
	"context"
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/deposit-service/internal/migration"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
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

// newCaseCommandFixture builds the adapter on a migrated deposit database
// without finance or workflow clients. Settlement skips posting when finance
// is absent, product and rate requests never post, borrow activation does
// not post; the approvals that post (interest ops, IBM placement and
// movements) are covered on their idempotent already-applied paths and on
// rejection.
func newCaseCommandFixture(t *testing.T) (*CaseCommandServer, *sql.DB) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	repo := repository.NewDepositRepository(db)
	settlement := service.NewSettlementService(repo, db, nil, nil)
	products := service.NewProductRequestService(repo, nil)
	ibm := service.NewIBMService(repo, db, nil, nil)
	interest := service.NewInterestService(repo, db, nil, nil)
	return NewCaseCommandServer(settlement, products, ibm, interest), db
}

func insertSavings(t *testing.T, db *sql.DB, code, savingsStatus string) string {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO dpm_products (id, tenant_id, code, name, term_months, interest_rate)
		VALUES ('prod-test', $1, 'TK6', 'Tiet kiem 6 thang', 6, 5.0) ON CONFLICT (id) DO NOTHING`, caseTestTenant); err != nil {
		t.Fatal(err)
	}
	id := repository.NewDepositID("dpm")
	if _, err := db.Exec(`
		INSERT INTO dpm_savings (id, tenant_id, savings_code, customer_code, product_code, open_date, maturity_date, principal_minor, accrued_minor, status, created_by)
		VALUES ($1, $2, $3, 'KH-1', 'prod-test', CURRENT_DATE, CURRENT_DATE + 180, 5000000, 20000, $4, 'tester')`,
		id, caseTestTenant, code, savingsStatus); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertProductRequest(t *testing.T, db *sql.DB, code, requestStatus string) string {
	t.Helper()
	id := repository.NewDepositID("dpmpr")
	if _, err := db.Exec(`
		INSERT INTO dpm_product_requests (id, tenant_id, request_type, product_code, name, term_months, interest_rate, status, created_by)
		VALUES ($1, $2, 'REGISTER', $3, $4, 12, 6.5, $5, 'tester')`,
		id, caseTestTenant, code, "San pham "+code, requestStatus); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertRateRequest(t *testing.T, db *sql.DB, requestStatus string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO dpm_rate_requests (tenant_id, request_type, payload, status, created_by)
		VALUES ($1, 'REGISTER', '{"product_code":"TK6","term_months":6,"rate":5.5,"effective_from":"2026-10-01"}', $2, 'tester')
		RETURNING id::text`, caseTestTenant, requestStatus).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertInterestOp(t *testing.T, db *sql.DB, savingsID, savingsCode, opStatus string, caseID *string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO dpm_interest_ops (tenant_id, savings_id, savings_code, op_type, amount_minor, status, created_by, workflow_case_id)
		VALUES ($1, $2, $3, 'PAY', 10000, $4, 'tester', $5::uuid) RETURNING id::text`,
		caseTestTenant, savingsID, savingsCode, opStatus, caseID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertIBMDeposit(t *testing.T, db *sql.DB, code, depositStatus string) string {
	t.Helper()
	id := repository.NewDepositID("ibm")
	if _, err := db.Exec(`
		INSERT INTO ibm_deposits (id, tenant_id, deposit_code, counterparty_code, deposit_date, maturity_date, principal_minor, interest_rate, status, created_by)
		VALUES ($1, $2, $3, 'TCTD-1', CURRENT_DATE, CURRENT_DATE + 90, 100000000, 4.2, $4, 'tester')`,
		id, caseTestTenant, code, depositStatus); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertIBMMovement(t *testing.T, db *sql.DB, depositID, kind, movementStatus string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO ibm_movements (tenant_id, deposit_id, kind, amount_minor, movement_date, status, created_by)
		VALUES ($1, $2, $3, 500000, CURRENT_DATE, $4, 'tester') RETURNING id::text`,
		caseTestTenant, depositID, kind, movementStatus).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertIBMBorrow(t *testing.T, db *sql.DB, code, borrowStatus string) string {
	t.Helper()
	id := repository.NewDepositID("ibmb")
	if _, err := db.Exec(`
		INSERT INTO ibm_borrows (id, tenant_id, borrow_code, counterparty_code, drawdown_date, maturity_date, principal_minor, outstanding_minor, status)
		VALUES ($1, $2, $3, 'TCTD-2', CURRENT_DATE, CURRENT_DATE + 180, 50000000, 50000000, $4)`, id, caseTestTenant, code, borrowStatus); err != nil {
		t.Fatal(err)
	}
	return id
}

func rowStatus(t *testing.T, db *sql.DB, table, idColumn, id string) string {
	t.Helper()
	var current string
	if err := db.QueryRow(`SELECT status FROM `+table+` WHERE `+idColumn+` = $1`, id).Scan(&current); err != nil {
		t.Fatal(err)
	}
	return current
}

func check(ctx context.Context, server *CaseCommandServer, kind, objectID, caseID string) (*commonv1.CheckCaseResponse, error) {
	return server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: kind, ObjectId: objectID, CaseId: caseID})
}

func resolve(ctx context.Context, server *CaseCommandServer, kind, objectID, caseID string, decision commonv1.CaseDecision, dataVersion string) (*commonv1.ResolveCaseResponse, error) {
	return server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{
		Kind: kind, ObjectId: objectID, CaseId: caseID, Decision: decision, DecidedBy: "checker-1", DataVersion: dataVersion,
	})
}

const (
	approve = commonv1.CaseDecision_CASE_DECISION_APPROVE
	reject  = commonv1.CaseDecision_CASE_DECISION_REJECT
)

func TestCaseCommandSettleChecksApprovesAndRejects(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	insertSavings(t, db, "STK-1", "ACTIVE")
	insertSavings(t, db, "STK-2", "ACTIVE")
	insertSavings(t, db, "STK-CLOSED", "CLOSED")
	ctx := tenantContext(caseTestTenant)

	if resp, err := check(ctx, server, KindSettle, "STK-1", ""); err != nil || !resp.GetOk() {
		t.Fatalf("active savings: %v %v", resp, err)
	}
	if resp, err := check(ctx, server, KindSettle, "STK-CLOSED", ""); err != nil || resp.GetOk() || resp.GetMessage() == "" {
		t.Fatalf("closed savings must fail the check: %v %v", resp, err)
	}
	if resp, err := check(tenantContext("other-tenant"), server, KindSettle, "STK-1", ""); err != nil || resp.GetOk() {
		t.Fatalf("another tenant must not see the savings: %v %v", resp, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindSettle, "STK-1", "", approve, "1"); err != nil {
			t.Fatalf("approve #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "dpm_savings", "savings_code", "STK-1"); got != "CLOSED" {
		t.Fatalf("savings status = %s", got)
	}
	if _, err := resolve(ctx, server, KindSettle, "STK-1", "", reject, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting a settled savings = %v, want FailedPrecondition", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindSettle, "STK-2", "", reject, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "dpm_savings", "savings_code", "STK-2"); got != "ACTIVE" {
		t.Fatalf("rejected settlement must leave the savings ACTIVE, got %s", got)
	}
	if _, err := resolve(ctx, server, KindSettle, "STK-2", "", approve, "9"); status.Code(err) != codes.Aborted {
		t.Fatalf("stale data version = %v, want Aborted", err)
	}
	if _, err := resolve(ctx, server, KindSettle, "STK-MISSING", "", reject, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("missing savings = %v, want NotFound", err)
	}
}

func TestCaseCommandProductRequestIsIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	toApprove := insertProductRequest(t, db, "SP-A", "SUBMITTED")
	toReject := insertProductRequest(t, db, "SP-R", "SUBMITTED")
	ctx := tenantContext(caseTestTenant)

	if resp, err := check(ctx, server, KindProductRequest, toApprove, ""); err != nil || !resp.GetOk() {
		t.Fatalf("submitted request: %v %v", resp, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindProductRequest, toApprove, "", approve, "1"); err != nil {
			t.Fatalf("approve #%d: %v", i+1, err)
		}
	}
	var products int
	if err := db.QueryRow(`SELECT count(*) FROM dpm_products WHERE tenant_id = $1 AND code = 'SP-A'`, caseTestTenant).Scan(&products); err != nil {
		t.Fatal(err)
	}
	if products != 1 || rowStatus(t, db, "dpm_product_requests", "id", toApprove) != "APPLIED" {
		t.Fatalf("products=%d status=%s, want one product and APPLIED", products, rowStatus(t, db, "dpm_product_requests", "id", toApprove))
	}
	if _, err := resolve(ctx, server, KindProductRequest, toApprove, "", reject, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an applied request = %v, want FailedPrecondition", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindProductRequest, toReject, "", reject, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "dpm_product_requests", "id", toReject); got != "REJECTED" {
		t.Fatalf("status = %s", got)
	}
	if _, err := resolve(ctx, server, KindProductRequest, toReject, "", approve, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected request = %v, want FailedPrecondition", err)
	}
	if _, err := resolve(tenantContext("other-tenant"), server, KindProductRequest, toReject, "", reject, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("another tenant = %v, want NotFound", err)
	}
}

func TestCaseCommandRateRequestIsIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	toApprove := insertRateRequest(t, db, "SUBMITTED")
	toReject := insertRateRequest(t, db, "SUBMITTED")
	ctx := tenantContext(caseTestTenant)

	if resp, err := check(ctx, server, KindRateRequest, toApprove, ""); err != nil || !resp.GetOk() {
		t.Fatalf("submitted request: %v %v", resp, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindRateRequest, toApprove, "", approve, "1"); err != nil {
			t.Fatalf("approve #%d: %v", i+1, err)
		}
	}
	var rates int
	if err := db.QueryRow(`SELECT count(*) FROM dpm_interest_rates WHERE tenant_id = $1`, caseTestTenant).Scan(&rates); err != nil {
		t.Fatal(err)
	}
	if rates != 1 || rowStatus(t, db, "dpm_rate_requests", "id::text", toApprove) != "APPLIED" {
		t.Fatalf("rates=%d, want one tier and APPLIED", rates)
	}
	if _, err := resolve(ctx, server, KindRateRequest, toApprove, "", reject, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an applied request = %v, want FailedPrecondition", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindRateRequest, toReject, "", reject, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if _, err := resolve(ctx, server, KindRateRequest, toReject, "", approve, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected request = %v, want FailedPrecondition", err)
	}
	if _, err := resolve(ctx, server, KindRateRequest, toReject, "", approve, "x"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("malformed data version = %v, want InvalidArgument", err)
	}
}

func TestCaseCommandInterestOpAndBatch(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	savingsID := insertSavings(t, db, "STK-L", "ACTIVE")
	caseID := "018f0000-0000-7000-8000-000000000001"
	submitted := insertInterestOp(t, db, savingsID, "STK-L", "SUBMITTED", &caseID)
	posted := insertInterestOp(t, db, savingsID, "STK-L", "POSTED", &caseID)
	loose := insertInterestOp(t, db, savingsID, "STK-L", "SUBMITTED", nil)
	ctx := tenantContext(caseTestTenant)

	if resp, err := check(ctx, server, KindInterestOp, submitted, ""); err != nil || !resp.GetOk() {
		t.Fatalf("submitted op: %v %v", resp, err)
	}
	if resp, err := check(ctx, server, KindInterestBatch, submitted, caseID); err != nil || resp.GetOk() || resp.GetMessage() == "" {
		t.Fatalf("batch with a POSTED op must fail the check with the savings in the message: %v %v", resp, err)
	}
	if resp, err := check(ctx, server, KindInterestBatch, submitted, "018f0000-0000-7000-8000-0000000000ff"); err != nil || resp.GetOk() {
		t.Fatalf("batch without ops must fail the check: %v %v", resp, err)
	}
	// A POSTED op is the retried approval: nothing is posted again.
	if _, err := resolve(ctx, server, KindInterestOp, posted, "", approve, ""); err != nil {
		t.Fatalf("retried approve of a posted op: %v", err)
	}
	if _, err := resolve(ctx, server, KindInterestOp, posted, "", reject, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting a posted op = %v, want FailedPrecondition", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindInterestOp, loose, "", reject, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "dpm_interest_ops", "id::text", loose); got != "REJECTED" {
		t.Fatalf("op status = %s", got)
	}
	// Batch rejection rejects the pending op and leaves the posted one alone
	// only if every op accepts the decision; a POSTED op refuses REJECT.
	if _, err := resolve(ctx, server, KindInterestBatch, submitted, caseID, reject, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("batch reject with a posted op = %v, want FailedPrecondition", err)
	}
	if got := rowStatus(t, db, "dpm_interest_ops", "id::text", submitted); got != "REJECTED" {
		t.Fatalf("pending op of the batch = %s, want REJECTED before the posted op refused", got)
	}
	if _, err := resolve(ctx, server, KindInterestBatch, submitted, "018f0000-0000-7000-8000-0000000000ff", approve, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("batch without ops = %v, want NotFound", err)
	}
}

func TestCaseCommandIBMKinds(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	pendingDeposit := insertIBMDeposit(t, db, "IBM-P", "PENDING_APPROVAL")
	activeDeposit := insertIBMDeposit(t, db, "IBM-A", "ACTIVE")
	topUp := insertIBMMovement(t, db, activeDeposit, "TOP_UP", "SUBMITTED")
	postedTopUp := insertIBMMovement(t, db, activeDeposit, "TOP_UP", "POSTED")
	borrow := insertIBMBorrow(t, db, "VAY-1", "PENDING_APPROVAL")
	ctx := tenantContext(caseTestTenant)

	if resp, err := check(ctx, server, KindIBMPlace, pendingDeposit, ""); err != nil || !resp.GetOk() {
		t.Fatalf("pending placement: %v %v", resp, err)
	}
	if resp, err := check(ctx, server, KindIBMTopUp, topUp, ""); err != nil || !resp.GetOk() {
		t.Fatalf("submitted top-up: %v %v", resp, err)
	}
	if resp, err := check(ctx, server, KindIBMWithdraw, topUp, ""); err != nil || resp.GetOk() {
		t.Fatalf("kind mismatch must fail the check: %v %v", resp, err)
	}
	if resp, err := check(tenantContext("other-tenant"), server, KindIBMBorrow, borrow, ""); err != nil || resp.GetOk() {
		t.Fatalf("another tenant must not see the borrow: %v %v", resp, err)
	}
	// Placement rejection twice, then approval is refused.
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindIBMPlace, pendingDeposit, "", reject, ""); err != nil {
			t.Fatalf("reject placement #%d: %v", i+1, err)
		}
	}
	if _, err := resolve(ctx, server, KindIBMPlace, pendingDeposit, "", approve, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected placement = %v, want FailedPrecondition", err)
	}
	// An ACTIVE deposit is the retried placement approval: no second posting.
	if _, err := resolve(ctx, server, KindIBMPlace, activeDeposit, "", approve, ""); err != nil {
		t.Fatalf("retried placement approve: %v", err)
	}
	// Borrow activation does not post, so the full approval path runs.
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindIBMBorrow, borrow, "", approve, "1"); err != nil {
			t.Fatalf("approve borrow #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "ibm_borrows", "id", borrow); got != "ACTIVE" {
		t.Fatalf("borrow status = %s", got)
	}
	if _, err := resolve(ctx, server, KindIBMBorrow, borrow, "", reject, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an active borrow = %v, want FailedPrecondition", err)
	}
	// Movements: reject twice, a POSTED one is the retried approval.
	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindIBMTopUp, topUp, "", reject, ""); err != nil {
			t.Fatalf("reject top-up #%d: %v", i+1, err)
		}
	}
	if got := rowStatus(t, db, "ibm_movements", "id::text", topUp); got != "REJECTED" {
		t.Fatalf("movement status = %s", got)
	}
	if _, err := resolve(ctx, server, KindIBMTopUp, postedTopUp, "", approve, ""); err != nil {
		t.Fatalf("retried approve of a posted movement: %v", err)
	}
	if _, err := resolve(ctx, server, KindIBMTopUp, postedTopUp, "", reject, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting a posted movement = %v, want FailedPrecondition", err)
	}
	if _, err := resolve(ctx, server, KindIBMTopUp, "018f0000-0000-7000-8000-0000000000aa", "", reject, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("missing movement = %v, want NotFound", err)
	}
}

func TestCaseCommandRejectsUnknownKindAndInput(t *testing.T) {
	server := NewCaseCommandServer(nil, nil, nil, nil)
	ctx := tenantContext(caseTestTenant)
	if _, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: "hrm.employee-register", ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown kind = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindSettle, ObjectId: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty object = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindSettle, ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified decision = %v", err)
	}
	if _, err := server.ResolveCase(context.Background(), &commonv1.ResolveCaseRequest{Kind: KindSettle, ObjectId: "x", Decision: approve}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("missing tenant = %v", err)
	}
}
