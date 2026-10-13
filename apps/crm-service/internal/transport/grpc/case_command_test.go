package grpc

import (
	"context"
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/crm-service/internal/migration"
	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	"github.com/arda-labs/arda/apps/crm-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	caseTestTenant = "tenant-case-command"
	caseTestOrg    = "org-case-command"
)

func caseContext(tenantID, orgID string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(ardametadata.TenantID, tenantID, ardametadata.OrgID, orgID))
}

func newCaseCommandFixture(t *testing.T) (*CaseCommandServer, *sql.DB) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	customers := NewCustomerCommandServer(repository.NewCustomerRepository(db), repository.NewAmendmentRepository(db))
	members := service.NewMemberService(repository.NewMemberRepository(db), nil)
	return NewCaseCommandServer(customers, members), db
}

func insertCustomer(t *testing.T, db *sql.DB, id, status, identityNo string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO customers (id, name, email, status, tenant_id, org_id, identity_no)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, "Khach hang "+id, id+"@example.test", status, caseTestTenant, caseTestOrg, identityNo); err != nil {
		t.Fatal(err)
	}
}

func customerStatus(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(`SELECT status FROM customers WHERE id = $1`, id).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func insertMemberRequest(t *testing.T, db *sql.DB, memberCode, requestType string, amountMinor int64, requestStatus string) string {
	t.Helper()
	var memberID string
	if err := db.QueryRow(`
		INSERT INTO crm_members (tenant_id, member_code, customer_code, open_date, created_by)
		VALUES ($1, $2, $3, CURRENT_DATE, 'tester') RETURNING id::text`,
		caseTestTenant, memberCode, "KH-"+memberCode).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	var requestID string
	if err := db.QueryRow(`
		INSERT INTO crm_member_requests (tenant_id, member_id, request_type, amount_minor, status, created_by)
		VALUES ($1, $2::uuid, $3, $4, $5, 'tester') RETURNING id::text`,
		caseTestTenant, memberID, requestType, amountMinor, requestStatus).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	return requestID
}

func memberRequestState(t *testing.T, db *sql.DB, requestID string) (requestStatus string, totalCapital int64) {
	t.Helper()
	if err := db.QueryRow(`
		SELECT r.status, m.total_capital_minor
		FROM crm_member_requests r JOIN crm_members m ON m.id = r.member_id
		WHERE r.id = $1::uuid`, requestID).Scan(&requestStatus, &totalCapital); err != nil {
		t.Fatal(err)
	}
	return requestStatus, totalCapital
}

func resolve(ctx context.Context, server *CaseCommandServer, kind, objectID string, decision commonv1.CaseDecision, dataVersion string) (*commonv1.ResolveCaseResponse, error) {
	return server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{
		Kind: kind, ObjectId: objectID, Decision: decision, DecidedBy: "checker-1", Note: "note", DataVersion: dataVersion,
	})
}

func TestCaseCommandCustomerRegisterCheckFlagsDuplicateIdentity(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	insertCustomer(t, db, "cust-clean", "SUBMITTED", "ID-1")
	insertCustomer(t, db, "cust-dup", "SUBMITTED", "ID-2")
	insertCustomer(t, db, "cust-existing", "ACTIVE", "ID-2")
	ctx := caseContext(caseTestTenant, caseTestOrg)

	resp, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindCustomerRegister, ObjectId: "cust-clean"})
	if err != nil || !resp.GetOk() {
		t.Fatalf("clean customer: %v %v", resp, err)
	}
	resp, err = server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindCustomerRegister, ObjectId: "cust-dup"})
	if err != nil || resp.GetOk() || resp.GetMessage() == "" {
		t.Fatalf("duplicate identity must fail the check with a message: %v %v", resp, err)
	}
	if got := customerStatus(t, db, "cust-dup"); got != "NEEDS_CHANGES" {
		t.Fatalf("duplicate customer status = %s, want NEEDS_CHANGES as the legacy worker set", got)
	}
	resp, err = server.CheckCase(caseContext("other-tenant", caseTestOrg), &commonv1.CheckCaseRequest{Kind: KindCustomerRegister, ObjectId: "cust-clean"})
	if err != nil || resp.GetOk() {
		t.Fatalf("another tenant must not see the customer: %v %v", resp, err)
	}
	if _, err := server.CheckCase(context.Background(), &commonv1.CheckCaseRequest{Kind: KindCustomerRegister, ObjectId: "cust-clean"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("missing scope = %v, want PermissionDenied", err)
	}
}

func TestCaseCommandCustomerRegisterApproveIsIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	insertCustomer(t, db, "cust-a", "SUBMITTED", "ID-A")
	ctx := caseContext(caseTestTenant, caseTestOrg)

	first, err := resolve(ctx, server, KindCustomerRegister, "cust-a", commonv1.CaseDecision_CASE_DECISION_APPROVE, "")
	if err != nil || first.GetOutputs()["customerStatus"] != "ACTIVE" {
		t.Fatalf("approve: %v %v", first, err)
	}
	second, err := resolve(ctx, server, KindCustomerRegister, "cust-a", commonv1.CaseDecision_CASE_DECISION_APPROVE, "")
	if err != nil || second.GetOutputs()["customerStatus"] != "ACTIVE" {
		t.Fatalf("retried approve: %v %v", second, err)
	}
	if got := customerStatus(t, db, "cust-a"); got != "ACTIVE" {
		t.Fatalf("status = %s", got)
	}
	if _, err := resolve(ctx, server, KindCustomerRegister, "cust-a", commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an active customer = %v, want FailedPrecondition", err)
	}
}

func TestCaseCommandCustomerRegisterRejectIsIdempotentAndGuarded(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	insertCustomer(t, db, "cust-r", "SUBMITTED", "ID-R")
	ctx := caseContext(caseTestTenant, caseTestOrg)

	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindCustomerRegister, "cust-r", commonv1.CaseDecision_CASE_DECISION_REJECT, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if got := customerStatus(t, db, "cust-r"); got != "REJECTED" {
		t.Fatalf("status = %s", got)
	}
	if _, err := resolve(ctx, server, KindCustomerRegister, "cust-r", commonv1.CaseDecision_CASE_DECISION_APPROVE, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected customer = %v, want FailedPrecondition", err)
	}
	if _, err := resolve(ctx, server, KindCustomerRegister, "cust-missing", commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("missing customer = %v, want NotFound", err)
	}
	if _, err := resolve(caseContext("other-tenant", caseTestOrg), server, KindCustomerRegister, "cust-r", commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("another tenant = %v, want NotFound", err)
	}
}

func TestCaseCommandMemberRequestCheckAndApproveIsIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	requestID := insertMemberRequest(t, db, "TV-1", "REGISTER", 100_000, "SUBMITTED")
	ctx := caseContext(caseTestTenant, caseTestOrg)

	resp, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindMemberRequest, ObjectId: requestID})
	if err != nil || !resp.GetOk() {
		t.Fatalf("submitted request: %v %v", resp, err)
	}
	resp, err = server.CheckCase(caseContext("other-tenant", caseTestOrg), &commonv1.CheckCaseRequest{Kind: KindMemberRequest, ObjectId: requestID})
	if err != nil || resp.GetOk() {
		t.Fatalf("another tenant must not see the request: %v %v", resp, err)
	}
	for i := 0; i < 2; i++ {
		out, err := resolve(ctx, server, KindMemberRequest, requestID, commonv1.CaseDecision_CASE_DECISION_APPROVE, "1")
		if err != nil || out.GetOutputs()["memberRequestStatus"] != "APPROVED" {
			t.Fatalf("approve #%d: %v %v", i+1, out, err)
		}
	}
	if requestStatus, total := memberRequestState(t, db, requestID); requestStatus != "APPROVED" || total != 100_000 {
		t.Fatalf("request=%s total=%d, want APPROVED and the capital applied once", requestStatus, total)
	}
	if _, err := resolve(ctx, server, KindMemberRequest, requestID, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an approved request = %v, want FailedPrecondition", err)
	}
	resp, err = server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindMemberRequest, ObjectId: requestID})
	if err != nil || resp.GetOk() {
		t.Fatalf("decided request must fail the check: %v %v", resp, err)
	}
}

func TestCaseCommandMemberRequestRejectIsIdempotentAndGuarded(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	requestID := insertMemberRequest(t, db, "TV-2", "ADDITIONAL", 50_000, "SUBMITTED")
	staleID := insertMemberRequest(t, db, "TV-3", "ADDITIONAL", 50_000, "SUBMITTED")
	ctx := caseContext(caseTestTenant, caseTestOrg)

	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, KindMemberRequest, requestID, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if requestStatus, total := memberRequestState(t, db, requestID); requestStatus != "REJECTED" || total != 0 {
		t.Fatalf("request=%s total=%d, want REJECTED without capital", requestStatus, total)
	}
	if _, err := resolve(ctx, server, KindMemberRequest, requestID, commonv1.CaseDecision_CASE_DECISION_APPROVE, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected request = %v, want FailedPrecondition", err)
	}
	if _, err := resolve(ctx, server, KindMemberRequest, staleID, commonv1.CaseDecision_CASE_DECISION_APPROVE, "9"); status.Code(err) != codes.Aborted {
		t.Fatalf("stale data version = %v, want Aborted", err)
	}
	if _, err := resolve(ctx, server, KindMemberRequest, staleID, commonv1.CaseDecision_CASE_DECISION_APPROVE, "x"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("malformed data version = %v, want InvalidArgument", err)
	}
	if _, err := resolve(caseContext("other-tenant", caseTestOrg), server, KindMemberRequest, staleID, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("another tenant = %v, want NotFound", err)
	}
}

func TestCaseCommandRejectsUnknownKindAndInput(t *testing.T) {
	server := NewCaseCommandServer(NewCustomerCommandServer(nil, nil), service.NewMemberService(nil, nil))
	ctx := caseContext(caseTestTenant, caseTestOrg)
	if _, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: "dpm.settle", ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown kind = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindMemberRequest, ObjectId: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty object = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindMemberRequest, ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified decision = %v", err)
	}
}
