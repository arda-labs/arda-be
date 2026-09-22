package service

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// IBM borrow lifecycle (tiền vay TCTD khác). Mirrors the deposit side but stays
// deliberately lighter: a submission stages a PENDING_APPROVAL borrow and a
// checker APPROVE activates it, which preserves segregation of duties at the
// API boundary without a BPMN case. The workflow case columns are already on the
// table, so wiring the maker-checker engine later needs no schema change.
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
