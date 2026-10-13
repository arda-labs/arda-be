package grpc

import (
	"context"
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/migration"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	"github.com/arda-labs/arda/apps/statistical-service/internal/service"
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
	return NewCaseCommandServer(service.NewStatisticalService(repository.NewStatisticalRepository(db), nil)), db
}

func insertSubmission(t *testing.T, db *sql.DB, reportCode, submissionStatus string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO rpt_report_submissions (tenant_id, report_code, period_code, status, created_by)
		VALUES ($1, $2, '2026-09', $3, 'tester') RETURNING id::text`,
		caseTestTenant, reportCode, submissionStatus).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func submissionState(t *testing.T, db *sql.DB, id string) (string, int64) {
	t.Helper()
	var current string
	var version int64
	if err := db.QueryRow(`SELECT status, version FROM rpt_report_submissions WHERE id = $1::uuid`, id).Scan(&current, &version); err != nil {
		t.Fatal(err)
	}
	return current, version
}

func resolve(ctx context.Context, server *CaseCommandServer, objectID string, decision commonv1.CaseDecision, dataVersion string) (*commonv1.ResolveCaseResponse, error) {
	return server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{
		Kind: KindReportSubmit, ObjectId: objectID, Decision: decision, DecidedBy: "checker-1", DataVersion: dataVersion,
	})
}

func TestCaseCommandCheckReportsActionableSubmissions(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	submitted := insertSubmission(t, db, "RPT-1", "SUBMITTED")
	approved := insertSubmission(t, db, "RPT-2", "APPROVED")
	ctx := tenantContext(caseTestTenant)

	resp, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindReportSubmit, ObjectId: submitted})
	if err != nil || !resp.GetOk() {
		t.Fatalf("submitted: %v %v", resp, err)
	}
	resp, err = server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: KindReportSubmit, ObjectId: approved})
	if err != nil || resp.GetOk() || resp.GetMessage() == "" {
		t.Fatalf("approved submission must fail the check with a message: %v %v", resp, err)
	}
	resp, err = server.CheckCase(tenantContext("other-tenant"), &commonv1.CheckCaseRequest{Kind: KindReportSubmit, ObjectId: submitted})
	if err != nil || resp.GetOk() {
		t.Fatalf("another tenant must not see the submission: %v %v", resp, err)
	}
}

func TestCaseCommandApproveIsIdempotent(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	id := insertSubmission(t, db, "RPT-A", "SUBMITTED")
	ctx := tenantContext(caseTestTenant)

	for i := 0; i < 2; i++ {
		out, err := resolve(ctx, server, id, commonv1.CaseDecision_CASE_DECISION_APPROVE, "1")
		if err != nil || out.GetOutputs()["submissionDecision"] != "APPROVE" {
			t.Fatalf("approve #%d: %v %v", i+1, out, err)
		}
	}
	if current, version := submissionState(t, db, id); current != "APPROVED" || version != 2 {
		t.Fatalf("status=%s version=%d, want APPROVED applied once", current, version)
	}
	if _, err := resolve(ctx, server, id, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejecting an approved submission = %v, want FailedPrecondition", err)
	}
}

func TestCaseCommandRejectIsIdempotentAndGuarded(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	id := insertSubmission(t, db, "RPT-R", "SUBMITTED")
	stale := insertSubmission(t, db, "RPT-S", "SUBMITTED")
	ctx := tenantContext(caseTestTenant)

	for i := 0; i < 2; i++ {
		if _, err := resolve(ctx, server, id, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); err != nil {
			t.Fatalf("reject #%d: %v", i+1, err)
		}
	}
	if current, _ := submissionState(t, db, id); current != "REJECTED" {
		t.Fatalf("status = %s", current)
	}
	if _, err := resolve(ctx, server, id, commonv1.CaseDecision_CASE_DECISION_APPROVE, ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("approving a rejected submission = %v, want FailedPrecondition", err)
	}
	if _, err := resolve(ctx, server, stale, commonv1.CaseDecision_CASE_DECISION_APPROVE, "9"); status.Code(err) != codes.Aborted {
		t.Fatalf("stale data version = %v, want Aborted", err)
	}
	if _, err := resolve(tenantContext("other-tenant"), server, stale, commonv1.CaseDecision_CASE_DECISION_REJECT, ""); status.Code(err) != codes.NotFound {
		t.Fatalf("another tenant = %v, want NotFound", err)
	}
}

func TestCaseCommandRejectsUnknownKindAndInput(t *testing.T) {
	server := NewCaseCommandServer(service.NewStatisticalService(nil, nil))
	ctx := tenantContext(caseTestTenant)
	if _, err := server.CheckCase(ctx, &commonv1.CheckCaseRequest{Kind: "dpm.settle", ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown kind = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindReportSubmit, ObjectId: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty object = %v", err)
	}
	if _, err := server.ResolveCase(ctx, &commonv1.ResolveCaseRequest{Kind: KindReportSubmit, ObjectId: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified decision = %v", err)
	}
	if _, err := server.ResolveCase(context.Background(), &commonv1.ResolveCaseRequest{Kind: KindReportSubmit, ObjectId: "x", Decision: commonv1.CaseDecision_CASE_DECISION_APPROVE}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("missing tenant = %v", err)
	}
}
