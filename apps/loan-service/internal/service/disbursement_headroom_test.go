package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/apps/loan-service/migrations"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
	"github.com/pressly/goose/v3"
)

const headroomTenantID = "00000000-0000-0000-0000-000000000021"

type headroomWorkflow struct {
	mu     sync.Mutex
	nextID int
}

func (f *headroomWorkflow) CreateCase(_ context.Context, _ workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	return &workflowv1.BusinessCase{Id: fmt.Sprintf("00000000-0000-4000-8000-%012d", f.nextID)}, nil
}

func (*headroomWorkflow) SubmitCase(_ context.Context, caseID, _ string, _ map[string]any, _ string) (*workflowv1.BusinessCase, error) {
	return &workflowv1.BusinessCase{Id: caseID}, nil
}

func openHeadroomFixture(t *testing.T) (*sql.DB, *repository.LoanRepository, string, string) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	t.Cleanup(func() { _ = db.Close() })
	repo := repository.NewLoanRepository(db)
	run := time.Now().UTC().Format("20060102T150405.000000000")
	contractCode, agreementCode := "HR-C-"+run, "HR-A-"+run
	ctx := context.Background()
	if _, err := repo.CreateContract(ctx, &domain.Contract{
		ID: repository.NewID("ctrt"), TenantID: headroomTenantID, ContractCode: contractCode,
		CustomerCode: "HR-CUST", LoanAmt: 1_000, LoanTerm: 12, TermUnit: "MONTH",
		ContractDate: "2026-10-01", MaturityDate: "2027-10-01", Status: domain.ContractDraft, CreatedBy: "test",
	}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := repo.CreateAgreement(ctx, &domain.Agreement{
		ID: repository.NewID("agr"), TenantID: headroomTenantID, ContractCode: contractCode,
		AgreementCode: agreementCode, DisburseDate: "2026-10-01", LoanTerm: 12,
		TermUnit: "MONTH", MaturityDate: "2027-10-01", Status: domain.AgreementActive, CreatedBy: "test",
	}); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	return db, repo, contractCode, agreementCode
}

func batchRegisterInput(contractCode, agreementCode string, amount int64) *CreateBatchInput {
	return &CreateBatchInput{
		TxnDate: "2026-10-01",
		Rows:    []BatchRowInput{{ContractCode: contractCode, AgreementCode: agreementCode, AmountMinor: amount}},
	}
}

func TestBatchRegisterHeadroom_NotDoubleCounted(t *testing.T) {
	db, repo, contractCode, agreementCode := openHeadroomFixture(t)
	// Under the corrected REGISTER model this is the state represented by the
	// legacy duplicate 400/400 balance after migration normalization.
	if _, err := db.Exec(`UPDATE lnm_agreements SET pending_disburse_amt_minor = 400
		WHERE tenant_id = $1 AND agreement_code = $2`, headroomTenantID, agreementCode); err != nil {
		t.Fatalf("seed in-transit drawdown: %v", err)
	}
	svc := NewBatchDisbursementService(repo, &headroomWorkflow{})
	if _, err := svc.CreateBatchRegister(context.Background(), headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 500)); err != nil {
		t.Fatalf("batch should fit after counting pending once: %v", err)
	}
}

func TestLegacyRegisterExposureMigration_NormalizesPending(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error {
		goose.SetBaseFS(migrations.FS)
		if err := goose.SetDialect("postgres"); err != nil {
			return err
		}
		return goose.UpTo(db, ".", 20260911140000, goose.WithAllowMissing())
	})
	defer db.Close()
	repo := repository.NewLoanRepository(db)
	run := time.Now().UTC().Format("20060102T150405.000000000")
	contractCode, agreementCode := "HR-LEGACY-C-"+run, "HR-LEGACY-A-"+run
	ctx := context.Background()
	if _, err := repo.CreateContract(ctx, &domain.Contract{
		ID: repository.NewID("ctrt"), TenantID: headroomTenantID, ContractCode: contractCode,
		CustomerCode: "HR-CUST", LoanAmt: 1_000, LoanTerm: 12, TermUnit: "MONTH",
		ContractDate: "2026-10-01", MaturityDate: "2027-10-01", Status: "PENDING", CreatedBy: "test",
	}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := repo.CreateAgreement(ctx, &domain.Agreement{
		ID: repository.NewID("agr"), TenantID: headroomTenantID, ContractCode: contractCode,
		AgreementCode: agreementCode, DisburseDate: "2026-10-01", LoanTerm: 12,
		TermUnit: "MONTH", MaturityDate: "2027-10-01", Status: "PENDING", CreatedBy: "test",
	}); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	if _, err := db.Exec(`UPDATE lnm_agreements
		SET outstanding_amt_minor = 400, pending_disburse_amt_minor = 400
		WHERE tenant_id = $1 AND agreement_code = $2`, headroomTenantID, agreementCode); err != nil {
		t.Fatalf("seed legacy duplicate balance: %v", err)
	}
	var postedID, submittedID string
	if err := db.QueryRow(`INSERT INTO lnm_disbursements
		(tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor, currency_code, status, created_by)
		VALUES ($1,$2,$3,'2026-10-01',400,'VND','POSTED','test') RETURNING id::text`,
		headroomTenantID, contractCode, agreementCode).Scan(&postedID); err != nil {
		t.Fatalf("seed posted register row: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO lnm_disbursements
		(tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor, currency_code, status, created_by)
		VALUES ($1,$2,$3,'2026-10-01',100,'VND','SUBMITTED','test') RETURNING id::text`,
		headroomTenantID, contractCode, agreementCode).Scan(&submittedID); err != nil {
		t.Fatalf("seed unresolved register row: %v", err)
	}
	if err := migration.Run(db, "postgres"); err != nil {
		t.Fatalf("apply exposure normalization migration: %v", err)
	}
	var contractStatus, agreementStatus string
	if err := db.QueryRow(`SELECT status FROM lnm_contracts WHERE tenant_id = $1 AND contract_code = $2`, headroomTenantID, contractCode).Scan(&contractStatus); err != nil {
		t.Fatalf("read normalized contract status: %v", err)
	}
	if contractStatus != domain.ContractPendingApproval {
		t.Fatalf("normalized contract status = %s, want PENDING_APPROVAL", contractStatus)
	}
	if err := db.QueryRow(`SELECT status FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`, headroomTenantID, agreementCode).Scan(&agreementStatus); err != nil {
		t.Fatalf("read normalized agreement status: %v", err)
	}
	if agreementStatus != domain.AgreementActive {
		t.Fatalf("normalized agreement status = %s, want ACTIVE", agreementStatus)
	}
	var outstanding, pending int64
	if err := db.QueryRow(`SELECT outstanding_amt_minor, pending_disburse_amt_minor
		FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`, headroomTenantID, agreementCode).
		Scan(&outstanding, &pending); err != nil {
		t.Fatalf("read normalized balance: %v", err)
	}
	if outstanding != 0 || pending != 400 {
		t.Fatalf("normalized outstanding/pending = %d/%d, want 0/400", outstanding, pending)
	}
	var postedReservation, submittedReservation string
	if err := db.QueryRow(`SELECT status FROM lnm_contract_reservations WHERE source_id = $1`, postedID).Scan(&postedReservation); err != nil {
		t.Fatalf("read migrated posted reservation: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM lnm_contract_reservations WHERE source_id = $1`, submittedID).Scan(&submittedReservation); err != nil {
		t.Fatalf("read migrated submitted reservation: %v", err)
	}
	if postedReservation != "CONSUMED" || submittedReservation != "HELD" {
		t.Fatalf("migrated reservation states = %s/%s, want CONSUMED/HELD", postedReservation, submittedReservation)
	}
	svc := NewBatchDisbursementService(repo, &headroomWorkflow{})
	if _, err := svc.CreateBatchRegister(ctx, headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 500)); err != nil {
		t.Fatalf("batch should fit after normalizing legacy 400/400 balance: %v", err)
	}
}

func TestHeadroom_SameForSingleAndBatch(t *testing.T) {
	_, repo, contractCode, agreementCode := openHeadroomFixture(t)
	single := NewDisbursementService(repo, nil)
	if _, err := single.Create(context.Background(), headroomTenantID, "test", &domain.Disbursement{
		ContractCode: contractCode, AgreementCode: agreementCode, DisburseDate: "2026-10-01",
		DisburseAmtMinor: 600, FlowType: domain.FlowRegister,
	}); err != nil {
		t.Fatalf("create first single reservation: %v", err)
	}
	if _, err := single.Create(context.Background(), headroomTenantID, "test", &domain.Disbursement{
		ContractCode: contractCode, AgreementCode: agreementCode, DisburseDate: "2026-10-01",
		DisburseAmtMinor: 401, FlowType: domain.FlowRegister,
	}); err == nil {
		t.Fatal("single path accepted 401 after 600 was reserved")
	}
	batch := NewBatchDisbursementService(repo, &headroomWorkflow{})
	if _, err := batch.CreateBatchRegister(context.Background(), headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 401)); err == nil {
		t.Fatal("batch path accepted 401 after 600 was reserved by single path")
	}
}

func TestRegisterReservationReleasedOnReject(t *testing.T) {
	db, repo, contractCode, agreementCode := openHeadroomFixture(t)
	svc := NewDisbursementService(repo, &fakeWorkflow{})
	item, err := svc.Create(context.Background(), headroomTenantID, "test", &domain.Disbursement{
		ContractCode: contractCode, AgreementCode: agreementCode, DisburseDate: "2026-10-01",
		DisburseAmtMinor: 600, FlowType: domain.FlowRegister,
	})
	if err != nil {
		t.Fatalf("create register: %v", err)
	}
	if _, err := svc.Submit(context.Background(), headroomTenantID, "test", item.ID); err != nil {
		t.Fatalf("submit register: %v", err)
	}
	if err := svc.Resolve(context.Background(), headroomTenantID, item.ID, "REJECT", "checker", "rejected by checker"); err != nil {
		t.Fatalf("reject register: %v", err)
	}
	var held, released int
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE status = 'HELD'), count(*) FILTER (WHERE status = 'RELEASED')
		FROM lnm_contract_reservations WHERE tenant_id = $1 AND source_id = $2`, headroomTenantID, item.ID).
		Scan(&held, &released); err != nil {
		t.Fatalf("read reservation state: %v", err)
	}
	if held != 0 || released != 1 {
		t.Fatalf("held/released reservations = %d/%d, want 0/1", held, released)
	}
}

func TestBatchRegisterReservationReleasedOnReject(t *testing.T) {
	db, repo, contractCode, agreementCode := openHeadroomFixture(t)
	svc := NewBatchDisbursementService(repo, &headroomWorkflow{})
	batch, err := svc.CreateBatchRegister(context.Background(), headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 600))
	if err != nil {
		t.Fatalf("create batch register: %v", err)
	}
	if err := svc.Resolve(context.Background(), headroomTenantID, batch.ID, "CANCEL", "cancelled by maker"); err != nil {
		t.Fatalf("cancel batch: %v", err)
	}
	resolved, err := svc.Get(context.Background(), headroomTenantID, batch.ID)
	if err != nil {
		t.Fatalf("reload cancelled batch: %v", err)
	}
	if resolved.Status != domain.BatchCancelled {
		t.Fatalf("batch status = %s, want CANCELLED", resolved.Status)
	}
	var held, released int
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE r.status = 'HELD'), count(*) FILTER (WHERE r.status = 'RELEASED')
		FROM lnm_contract_reservations r JOIN lnm_disbursements d ON d.id = r.source_id
		WHERE r.tenant_id = $1 AND d.batch_id = $2`, headroomTenantID, batch.ID).
		Scan(&held, &released); err != nil {
		t.Fatalf("read batch reservation state: %v", err)
	}
	if held != 0 || released != 1 {
		t.Fatalf("held/released batch reservations = %d/%d, want 0/1", held, released)
	}
}

func TestTwoBatchesConcurrent_DoNotExceedLoanAmt(t *testing.T) {
	_, repo, contractCode, agreementCode := openHeadroomFixture(t)
	wf := &headroomWorkflow{}
	svc := NewBatchDisbursementService(repo, wf)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := svc.CreateBatchRegister(context.Background(), headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 600))
			results <- err
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else if !strings.Contains(strings.ToLower(err.Error()), "headroom") {
			t.Errorf("unexpected batch create error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful 600 batches = %d, want exactly 1", successes)
	}
}
