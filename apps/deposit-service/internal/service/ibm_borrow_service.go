package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
)

// IBM borrow lifecycle (tiền vay TCTD khác). A submission stages a
// PENDING_APPROVAL borrow and opens an IBM_BORROW_V1 maker-checker case (the
// placement BPMN shape with a different object); the case checker's APPROVE
// activates the contract. Deployments without a workflow client keep the staged
// row, and the DecideBorrow API still refuses a self-approval.
//
// The PCF "Tiền vay TCTD" indicators read the reporting projection, so the
// lifecycle here only has to keep status truthful.

// SubmitBorrow stages one interbank borrowing.
func (s *IBMService) SubmitBorrow(ctx context.Context, tenantID, actor string, in *repository.InterbankBorrow) (*repository.InterbankBorrow, error) {
	if strings.TrimSpace(in.BorrowCode) == "" || strings.TrimSpace(in.CounterpartyCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "borrow_code and counterparty_code are required")
	}
	if in.PrincipalMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "principal_minor must be positive")
	}
	if in.DrawdownDate == "" || in.MaturityDate == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "drawdown_date and maturity_date are required")
	}
	if in.TermMonths < 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "term_months cannot be negative")
	}
	in.ID = ""
	in.TenantID = tenantID
	in.Status = "PENDING_APPROVAL"
	in.CreatedBy = actor
	created, err := s.repo.CreateIBMBorrow(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	// Route through the same maker-checker gate as the placement side: the
	// contract stays PENDING_APPROVAL until a checker APPROVE. A deployment
	// without a workflow client keeps the staged row (the API gate in
	// DecideBorrow still refuses a self-approval), it just does not open a case.
	if s.workflow == nil {
		return created, nil
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseIBMBorrow,
		Title:             fmt.Sprintf("Đề nghị vay vốn TCTD khác %s", created.BorrowCode),
		PrimaryObjectType: "ibm.borrow",
		PrimaryObjectID:   created.ID,
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("ibm-borrow-%s", created.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"kind":       IBMKindBorrow,
		"refId":      created.ID,
		"borrowCode": created.BorrowCode,
	}, fmt.Sprintf("ibm-borrow-%s-submit", created.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetIBMBorrowCase(ctx, tenantID, created.ID, caseCreated.Id, actor); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	created.WorkflowCaseID = &caseCreated.Id
	return created, nil
}

// DecideBorrow approves or rejects a staged borrowing. Any decision other than
// approve retires the row (REJECTED) so it never reaches the reporting slice.
func (s *IBMService) DecideBorrow(ctx context.Context, tenantID, id, decision, actor, dataVersion string) (*repository.InterbankBorrow, error) {
	existing, err := s.repo.GetIBMBorrowByID(ctx, tenantID, id)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if existing == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "interbank borrow not found")
	}
	switch strings.ToUpper(strings.TrimSpace(decision)) {
	case "APPROVE":
		// An approver must not be the submitter: the whole point of the gate.
		if existing.CreatedBy != "" && existing.CreatedBy == actor {
			return nil, ardaerrors.New(ardaerrors.CodeForbidden, "the submitter cannot approve their own borrowing")
		}
		if existing.Status != "PENDING_APPROVAL" {
			return nil, ardaerrors.New(ardaerrors.CodeConflict, "borrow is not awaiting approval")
		}
		if err := s.repo.UpdateIBMBorrowStatus(ctx, tenantID, id, "ACTIVE", actor, dataVersion); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
		}
	case "REJECT":
		if existing.Status != "PENDING_APPROVAL" {
			return nil, ardaerrors.New(ardaerrors.CodeConflict, "borrow is not awaiting approval")
		}
		if err := s.repo.UpdateIBMBorrowStatus(ctx, tenantID, id, "REJECTED", actor, dataVersion); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
		}
	default:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
	return s.repo.GetIBMBorrowByID(ctx, tenantID, id)
}

// GetBorrowDetail returns the borrow aggregate (contract + movements).
func (s *IBMService) GetBorrowDetail(ctx context.Context, tenantID, id string) (*repository.InterbankBorrow, []repository.IBMBorrowMovement, error) {
	borrow, err := s.repo.GetIBMBorrowByID(ctx, tenantID, id)
	if err != nil {
		return nil, nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if borrow == nil {
		return nil, nil, ardaerrors.New(ardaerrors.CodeNotFound, "interbank borrow not found")
	}
	movements, err := s.repo.ListIBMBorrowMovementsByBorrow(ctx, tenantID, id)
	if err != nil {
		return nil, nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return borrow, movements, nil
}

// ListBorrows returns the tenant's borrow contracts.
func (s *IBMService) ListBorrows(ctx context.Context, tenantID string, orgCodes []string, status string) ([]repository.InterbankBorrow, error) {
	return s.repo.ListIBMBorrows(ctx, tenantID, orgCodes, status)
}

// SubmitBorrowMovement stages one movement against an ACTIVE borrow.
func (s *IBMService) SubmitBorrowMovement(ctx context.Context, tenantID, borrowID, actor string, in *repository.IBMBorrowMovement) (*repository.IBMBorrowMovement, error) {
	borrow, err := s.repo.GetIBMBorrowByID(ctx, tenantID, borrowID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if borrow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "interbank borrow not found")
	}
	if borrow.Status != "ACTIVE" {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, "borrow is not active")
	}
	switch strings.ToUpper(strings.TrimSpace(in.Kind)) {
	case "DRAWDOWN", "REPAYMENT", "INTEREST", "EARLY_REPAY":
	default:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "kind must be DRAWDOWN, REPAYMENT, INTEREST or EARLY_REPAY")
	}
	if in.AmountMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_minor must be positive")
	}
	if in.MovementDate == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "movement_date is required")
	}
	in.TenantID = tenantID
	in.BorrowID = borrowID
	in.Status = "SUBMITTED"
	in.CreatedBy = actor
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	created, err := s.repo.CreateIBMBorrowMovement(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// ListBorrowsForReporting is the statistical ETL read for the "Tiền vay TCTD"
// indicators.
func (s *IBMService) ListBorrowsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.IBMBorrowReportingRow, error) {
	return s.repo.ListIBMBorrowsForReporting(ctx, tenantID, orgCode)
}
