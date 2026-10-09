package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

func TestListDisbursements_ExcludesBatchRows(t *testing.T) {
	_, repo, contractCode, agreementCode := openHeadroomFixture(t)
	ctx := context.Background()
	singleSvc := NewDisbursementService(repo, &fakeWorkflow{})
	single, err := singleSvc.Create(ctx, headroomTenantID, "test", disbursementInput(contractCode, agreementCode, 100))
	if err != nil {
		t.Fatalf("create single disbursement: %v", err)
	}
	batchSvc := NewBatchDisbursementService(repo, &headroomWorkflow{})
	batch, err := batchSvc.CreateBatchRegister(ctx, headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 100))
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	items, total, err := singleSvc.List(ctx, headroomTenantID, nil, "", "", "", "", "", "", 1, 20)
	if err != nil {
		t.Fatalf("list single disbursements: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != single.ID {
		t.Fatalf("single list = total %d, items %+v; want only %s (batch %s)", total, items, single.ID, batch.ID)
	}
}

func TestSubmitDisbursement_RejectsBatchRow(t *testing.T) {
	_, repo, contractCode, agreementCode := openHeadroomFixture(t)
	ctx := context.Background()
	batchSvc := NewBatchDisbursementService(repo, &headroomWorkflow{})
	batch, err := batchSvc.CreateBatchRegister(ctx, headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 100))
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	singleSvc := NewDisbursementService(repo, &fakeWorkflow{})
	_, err = singleSvc.Submit(ctx, headroomTenantID, "test", batch.Rows[0].ID)
	var apiErr *ardaerrors.Error
	if err == nil || !errors.As(err, &apiErr) || apiErr.Code != ardaerrors.CodeConflict || !strings.Contains(strings.ToLower(err.Error()), "batch") {
		t.Fatalf("submit batch row error = %v, want conflict identifying batch ownership", err)
	}
}

func TestBatchReject_MarksRowsRejected(t *testing.T) {
	_, repo, contractCode, agreementCode := openHeadroomFixture(t)
	ctx := context.Background()
	svc := NewBatchDisbursementService(repo, &headroomWorkflow{})
	batch, err := svc.CreateBatchRegister(ctx, headroomTenantID, "test", "", batchRegisterInput(contractCode, agreementCode, 100))
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if _, err := svc.Submit(ctx, headroomTenantID, "test", batch.ID, batch.DataVersion); err != nil {
		t.Fatalf("submit batch: %v", err)
	}
	if err := svc.Resolve(ctx, headroomTenantID, batch.ID, "REJECT", "rejected by checker"); err != nil {
		t.Fatalf("reject batch: %v", err)
	}
	rows, err := repo.GetBatchRows(ctx, headroomTenantID, batch.ID)
	if err != nil {
		t.Fatalf("reload batch rows: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != "REJECTED" {
		t.Fatalf("batch rows after reject = %+v, want one REJECTED row", rows)
	}
}

func disbursementInput(contractCode, agreementCode string, amount int64) *domain.Disbursement {
	return &domain.Disbursement{
		ContractCode: contractCode, AgreementCode: agreementCode, DisburseDate: "2026-10-01",
		DisburseAmtMinor: amount, FlowType: domain.FlowRegister,
	}
}
