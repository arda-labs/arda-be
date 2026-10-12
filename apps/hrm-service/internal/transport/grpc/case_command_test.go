package grpc

import (
	"context"
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/hrm-service/internal/migration"
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

func newCaseCommandFixture(t *testing.T) (*CaseCommandServer, *sql.DB) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	return NewCaseCommandServer(NewEmployeeServer(db)), db
}

func insertRegistration(t *testing.T, db *sql.DB, id, code, status string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO hrm_employee_registrations (id, tenant_id, registration_code, payload, status)
		VALUES ($1, $2, $3, $4::jsonb, $5)`,
		id, caseTestTenant, code, `{"employee_code":"NV-`+id+`","full_name":"Nguyen Van `+id+`"}`, status); err != nil {
		t.Fatal(err)
	}
}

func registrationStatus(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(`SELECT status FROM hrm_employee_registrations WHERE id = $1`, id).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCaseCommandCheckReportsActionableRegistrations(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	insertRegistration(t, db, "reg-ok", "DKNV-1", "SUBMITTED")
	insertRegistration(t, db, "reg-done", "DKNV-2", "APPROVED")
	ctx := tenantContext(caseTestTenant)

	resp, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindEmployeeRegister, ObjectId: "reg-ok"})
	if err != nil || !resp.GetOk() {
		t.Fatalf("submitted registration: %v %v", resp, err)
	}
	resp, err = server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindEmployeeRegister, ObjectId: "reg-done"})
	if err != nil || resp.GetOk() || resp.GetMessage() == "" {
		t.Fatalf("approved registration must fail the check with a message: %v %v", resp, err)
	}
	resp, err = server.CheckCase(tenantContext("other-tenant"), &commonv1.CheckCaseRequest{Kind: KindEmployeeRegister, ObjectId: "reg-ok"})
	if err != nil || resp.GetOk() {
		t.Fatalf("another tenant must not see the registration: %v %v", resp, err)
	}
}

func TestCaseCommandApproveIsIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	insertRegistration(t, db, "reg-a", "DKNV-3", "IN_REVIEW")
	ctx := tenantContext(caseTestTenant)
	req := &commonv1.ResolveCaseRequest{Kind: KindEmployeeRegister, ObjectId: "reg-a", Decision: commonv1.CaseDecision_CASE_DECISION_APPROVE, DecidedBy: "checker-1"}

	first, err := server.ResolveCase(ctx, req)
	if err != nil || first.GetOutputs()["employeeId"] == "" {
		t.Fatalf("approve: %v %v", first, err)
	}
	second, err := server.ResolveCase(ctx, req)
	if err != nil || second.GetOutputs()["employeeId"] != first.GetOutputs()["employeeId"] {
		t.Fatalf("retried approve must return the same employee: first=%v second=%v err=%v", first, second, err)
	}
	var employees int
	if err := db.QueryRow(`SELECT count(*) FROM hrm_employees WHERE tenant_id = $1`, caseTestTenant).Scan(&employees); err != nil {
		t.Fatal(err)
	}
	if employees != 1 || registrationStatus(t, db, "reg-a") != "APPROVED" {
		t.Fatalf("employees=%d status=%s, want one employee and APPROVED", employees, registrationStatus(t, db, "reg-a"))
	}
}

func TestCaseCommandRejectIsIdempotentAndGuarded(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	insertRegistration(t, db, "reg-r", "DKNV-4", "SUBMITTED")
	insertRegistration(t, db, "reg-approved", "DKNV-5", "APPROVED")
	ctx := tenantContext(caseTestTenant)
	reject := func(id string) error {
		_, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindEmployeeRegister, ObjectId: id, Decision: commonv1.CaseDecision_CASE_DECISION_REJECT, DecidedBy: "checker-1", Note: "thiếu hồ sơ"})
		return err
	}
	if err := reject("reg-r"); err != nil {
		t.Fatal(err)
	}
	if err := reject("reg-r"); err != nil {
		t.Fatalf("retried reject must succeed: %v", err)
	}
	if got := registrationStatus(t, db, "reg-r"); got != "REJECTED" {
		t.Fatalf("status = %s", got)
	}
	if err := reject("reg-approved"); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an approved registration = %v, want FailedPrecondition", err)
	}
	if err := reject("reg-missing"); status.Code(err) != codes.NotFound {
		t.Fatalf("rejecting a missing registration = %v, want NotFound", err)
	}
}

func TestCaseCommandRejectsUnknownKindAndInput(t *testing.T) {
	server := NewCaseCommandServer(NewEmployeeServer(nil))
	ctx := tenantContext(caseTestTenant)
	if _, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: "dpm.settle", ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown kind = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindEmployeeRegister, ObjectId: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty object = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindEmployeeRegister, ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified decision = %v", err)
	}
}
