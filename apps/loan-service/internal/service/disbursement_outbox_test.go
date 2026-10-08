package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

type outboxWorkflow struct {
	mu          sync.Mutex
	createCalls int
	submitCalls int
	caseID      string
	variables   map[string]any
	failSubmit  bool
}

func (f *outboxWorkflow) CreateCase(_ context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if in.IdempotencyKey == "" {
		return nil, errors.New("missing idempotency key")
	}
	f.caseID = "00000000-0000-4000-8000-000000000123"
	return &workflowv1.BusinessCase{Id: f.caseID, CaseCode: "CASE-123"}, nil
}

func (f *outboxWorkflow) SubmitCase(_ context.Context, caseID, _ string, variables map[string]any, _ string) (*workflowv1.BusinessCase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls++
	f.variables = variables
	if f.failSubmit {
		return nil, errors.New("workflow unavailable")
	}
	if variables["caseId"] != caseID {
		return nil, fmt.Errorf("initial caseId variable %v does not match %s", variables["caseId"], caseID)
	}
	return &workflowv1.BusinessCase{Id: caseID}, nil
}

func TestDisbursementSubmitIsIdempotentAndWorkflowIsOutboxed(t *testing.T) {
	db, repo, contractCode, agreementCode := openHeadroomFixture(t)
	ctx := context.Background()
	wf := &outboxWorkflow{}
	svc := NewBatchDisbursementService(repo, wf)
	draft, err := svc.CreateBatchRegister(ctx, headroomTenantID, "maker", "", batchRegisterInput(contractCode, agreementCode, 100))
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	first, err := svc.Submit(ctx, headroomTenantID, "maker", draft.ID, draft.DataVersion)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	second, err := svc.Submit(ctx, headroomTenantID, "maker", draft.ID, draft.DataVersion)
	if err != nil {
		t.Fatalf("idempotent submit: %v", err)
	}
	if first.Status != domain.BatchSubmitted || second.Status != domain.BatchSubmitted {
		t.Fatalf("submit statuses = %s/%s", first.Status, second.Status)
	}
	if wf.createCalls != 0 || wf.submitCalls != 0 {
		t.Fatalf("HTTP submit called workflow %d/%d times", wf.createCalls, wf.submitCalls)
	}
	var outboxCount, held int
	if err := db.QueryRow(`SELECT count(*) FROM lnm_disbursement_workflow_outbox WHERE tenant_id=$1 AND batch_id=$2`, headroomTenantID, draft.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM lnm_contract_reservations r JOIN lnm_disbursements d ON d.id=r.source_id WHERE d.tenant_id=$1 AND d.batch_id=$2 AND r.status='HELD'`, headroomTenantID, draft.ID).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 || held != 1 {
		t.Fatalf("outbox/reservations = %d/%d, want 1/1", outboxCount, held)
	}
	if err := svc.ProcessWorkflowOutboxOnce(ctx); err != nil {
		t.Fatalf("relay outbox: %v", err)
	}
	if wf.createCalls != 1 || wf.submitCalls != 1 {
		t.Fatalf("relay workflow calls = %d/%d, want 1/1", wf.createCalls, wf.submitCalls)
	}
	if wf.variables["caseId"] != wf.caseID {
		t.Fatalf("caseId passed to start = %v, want %s", wf.variables["caseId"], wf.caseID)
	}
	loaded, err := svc.Get(ctx, headroomTenantID, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WorkflowCaseID == nil || *loaded.WorkflowCaseID != wf.caseID || len(loaded.History) < 3 {
		t.Fatalf("detail missing case/history: %+v", loaded)
	}
}

func TestDisbursementDraftUpdateUsesVersionAndCanBeCancelled(t *testing.T) {
	_, repo, contractCode, agreementCode := openHeadroomFixture(t)
	ctx := context.Background()
	svc := NewBatchDisbursementService(repo, &outboxWorkflow{})
	draft, err := svc.CreateBatchRegister(ctx, headroomTenantID, "maker", "", batchRegisterInput(contractCode, agreementCode, 100))
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	update := batchRegisterInput(contractCode, agreementCode, 125)
	update.DataVersion = draft.DataVersion
	updated, err := svc.UpdateDraft(ctx, headroomTenantID, "maker", draft.ID, update)
	if err != nil {
		t.Fatalf("update draft: %v", err)
	}
	if updated.TotalAmtMinor != 125 || updated.Rows[0].DisburseAmtMinor != 125 || updated.DataVersion <= draft.DataVersion {
		t.Fatalf("updated draft = %+v", updated)
	}
	update.DataVersion = draft.DataVersion
	if _, err := svc.UpdateDraft(ctx, headroomTenantID, "maker", draft.ID, update); err == nil {
		t.Fatal("stale draft update succeeded")
	}
	if err := svc.CancelDraft(ctx, headroomTenantID, "maker", draft.ID, updated.DataVersion); err != nil {
		t.Fatalf("cancel draft: %v", err)
	}
	cancelled, err := svc.Get(ctx, headroomTenantID, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != domain.BatchCancelled {
		t.Fatalf("cancelled status = %s", cancelled.Status)
	}
}

func TestDisbursementRegisterDraftCanBeSavedBeforeRowsAreAdded(t *testing.T) {
	_, repo, _, _ := openHeadroomFixture(t)
	ctx := context.Background()
	wf := &outboxWorkflow{}
	svc := NewBatchDisbursementService(repo, wf)
	input := &CreateBatchInput{TxnDate: "2026-10-08", PaymentMethod: "TRANSFER", Rows: []BatchRowInput{}}
	draft, err := svc.CreateBatchRegister(ctx, headroomTenantID, "maker", "", input)
	if err != nil {
		t.Fatalf("create empty register draft: %v", err)
	}
	if draft.Status != domain.BatchDraft || len(draft.Rows) != 0 {
		t.Fatalf("empty register draft = status %s, rows %d", draft.Status, len(draft.Rows))
	}
	input.DataVersion = draft.DataVersion
	updated, err := svc.UpdateDraft(ctx, headroomTenantID, "maker", draft.ID, input)
	if err != nil {
		t.Fatalf("update empty register draft: %v", err)
	}
	if updated.DataVersion <= draft.DataVersion || len(updated.Rows) != 0 {
		t.Fatalf("updated empty draft = version %d, rows %d", updated.DataVersion, len(updated.Rows))
	}
	if _, err := svc.Submit(ctx, headroomTenantID, "maker", draft.ID, updated.DataVersion); err == nil {
		t.Fatal("submit of empty register draft succeeded")
	}
	if wf.createCalls != 0 || wf.submitCalls != 0 {
		t.Fatalf("empty draft submit called workflow %d/%d times", wf.createCalls, wf.submitCalls)
	}
}

func TestDisbursementOutboxExhaustionCanBeResubmitted(t *testing.T) {
	db, repo, contractCode, agreementCode := openHeadroomFixture(t)
	ctx := context.Background()
	svc := NewBatchDisbursementService(repo, &outboxWorkflow{failSubmit: true})
	draft, err := svc.CreateBatchRegister(ctx, headroomTenantID, "maker", "", batchRegisterInput(contractCode, agreementCode, 100))
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := svc.Submit(ctx, headroomTenantID, "maker", draft.ID, draft.DataVersion); err != nil {
		t.Fatalf("submit: %v", err)
	}
	for attempt := 0; attempt < disbursementWorkflowMaxAttempts; attempt++ {
		claimed, err := repo.ClaimDisbursementWorkflowOutbox(ctx, 1)
		if err != nil {
			t.Fatalf("claim attempt %d: %v", attempt+1, err)
		}
		if len(claimed) != 1 {
			t.Fatalf("claim attempt %d returned %d rows", attempt+1, len(claimed))
		}
		if err := repo.FailDisbursementWorkflowOutbox(ctx, claimed[0], "workflow unavailable", disbursementWorkflowMaxAttempts); err != nil {
			t.Fatalf("fail attempt %d: %v", attempt+1, err)
		}
		if attempt+1 < disbursementWorkflowMaxAttempts {
			if _, err := db.Exec(`UPDATE lnm_disbursement_workflow_outbox SET next_attempt_at=now() WHERE tenant_id=$1 AND batch_id=$2`, headroomTenantID, draft.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	failed, err := svc.Get(ctx, headroomTenantID, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != domain.BatchSubmitFailed || failed.Rows[0].Status != domain.DisbursementDraft {
		t.Fatalf("failed batch/row statuses = %s/%s", failed.Status, failed.Rows[0].Status)
	}
	var held int
	if err := db.QueryRow(`SELECT count(*) FROM lnm_contract_reservations r JOIN lnm_disbursements d ON d.id=r.source_id WHERE d.tenant_id=$1 AND d.batch_id=$2 AND r.status='HELD'`, headroomTenantID, draft.ID).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held != 0 {
		t.Fatalf("failed submission retained %d reservations", held)
	}
	retry, err := svc.Submit(ctx, headroomTenantID, "maker", draft.ID, failed.DataVersion)
	if err != nil {
		t.Fatalf("resubmit failed batch: %v", err)
	}
	if retry.Status != domain.BatchSubmitted {
		t.Fatalf("resubmit status = %s", retry.Status)
	}
	var attemptCount int
	if err := db.QueryRow(`SELECT attempt_count FROM lnm_disbursement_workflow_outbox WHERE tenant_id=$1 AND batch_id=$2`, headroomTenantID, draft.ID).Scan(&attemptCount); err != nil {
		t.Fatal(err)
	}
	if attemptCount != 0 {
		t.Fatalf("resubmission attempts = %d, want reset to 0", attemptCount)
	}
}

func TestSubmitTwentyRowsLatencyBaseline(t *testing.T) {
	_, repo, contractCode, agreementCode := openHeadroomFixture(t)
	ctx := context.Background()
	wf := &outboxWorkflow{}
	svc := NewBatchDisbursementService(repo, wf)
	in := batchRegisterInput(contractCode, agreementCode, 1)
	in.Rows = make([]BatchRowInput, 20)
	for i := range in.Rows {
		in.Rows[i] = BatchRowInput{ContractCode: contractCode, AgreementCode: agreementCode, AmountMinor: 1}
	}
	draft, err := svc.CreateBatchRegister(ctx, headroomTenantID, "maker", "", in)
	if err != nil {
		t.Fatalf("create 20-row draft: %v", err)
	}
	started := time.Now()
	if _, err := svc.Submit(ctx, headroomTenantID, "maker", draft.ID, draft.DataVersion); err != nil {
		t.Fatalf("submit 20-row batch: %v", err)
	}
	elapsed := time.Since(started)
	if wf.createCalls != 0 || wf.submitCalls != 0 {
		t.Fatalf("submit synchronously called workflow %d/%d times", wf.createCalls, wf.submitCalls)
	}
	t.Logf("20-row submit: %s; SQL operation count is fixed (10 in transaction + 1 batch readback), workflow RPCs in request: 0", elapsed)
}

var _ interface {
	CreateCase(context.Context, workflowclient.CaseCreate) (*workflowv1.BusinessCase, error)
	SubmitCase(context.Context, string, string, map[string]any, string) (*workflowv1.BusinessCase, error)
} = (*outboxWorkflow)(nil)
