package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// AdditionalDepositService runs the DPM.301 additional-deposit flow: the
// maker submits a per-savings case, the checker approval bumps the principal
// and posts the same DR cash / CR deposit-liability shape as DPM_OPEN (the
// DPM_OPEN rule card is reused; the journal reference says DPM_ADDITIONAL).
type AdditionalDepositService struct {
	repo       *repository.DepositRepository
	settlement *SettlementService
	db         *sql.DB
	workflow   WorkflowSubmitter
}

func NewAdditionalDepositService(repo *repository.DepositRepository, settlement *SettlementService, db *sql.DB, workflow WorkflowSubmitter) *AdditionalDepositService {
	return &AdditionalDepositService{repo: repo, settlement: settlement, db: db, workflow: workflow}
}

// Submit creates + submits the DPM_ADDITIONAL_V1 maker/checker case.
func (s *AdditionalDepositService) Submit(ctx context.Context, tenantID, actor, savingsCode string, amountMinor int64, txnDate string) (*Submission, error) {
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	ok, message, err := s.Check(ctx, tenantID, savingsCode, amountMinor)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, message)
	}
	if txnDate == "" {
		txnDate = todayDep(ctx)
	} else if _, err := ardatime.ParseDay(txnDate); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "txn_date must be YYYY-MM-DD")
	}
	key := idempotencyKey("dpm-additional", savingsCode)
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          "DPM_ADDITIONAL_V1",
		Title:             "Nộp thêm sổ " + savingsCode,
		PrimaryObjectType: "dpm.savings",
		PrimaryObjectID:   savingsCode,
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    key,
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err := s.workflow.SubmitCase(ctx, caseCreated.GetId(), actor, map[string]any{
		"savingsCode":    savingsCode,
		"amountMinor":    amountMinor,
		"txnDate":        txnDate,
		"idempotencyKey": key,
	}, key+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	return &Submission{CaseID: caseCreated.GetId(), CaseCode: caseCreated.GetCaseCode()}, nil
}

// Check validates the savings is ACTIVE and the amount is positive (BPMN
// validate job + submit pre-check).
func (s *AdditionalDepositService) Check(ctx context.Context, tenantID, savingsCode string, amountMinor int64) (bool, string, error) {
	if amountMinor <= 0 {
		return false, "amount_minor must be positive", nil
	}
	savings, err := s.repo.GetSavingsByCode(ctx, tenantID, savingsCode)
	if err != nil {
		return false, "savings not found: " + savingsCode, nil
	}
	if savings.Status != "ACTIVE" {
		return false, fmt.Sprintf("status %s is not actionable", savings.Status), nil
	}
	return true, "ok", nil
}

// Settle posts the movement, bumps the principal and records the POSTED
// transaction. Idempotent per idempotency_key (finance replay) and safe on
// retry: the principal bump + txn insert run in one DB transaction.
//
// dataVersion is the savings row version the checker approved (as a string,
// from the domain read API). When set, a version that moved on refuses the
// bump — a stale approval must be re-reviewed, never silently applied.
func (s *AdditionalDepositService) Settle(ctx context.Context, tenantID, actor, savingsCode string, amountMinor int64, txnDate, idemKey, dataVersion string) error {
	ok, message, err := s.Check(ctx, tenantID, savingsCode, amountMinor)
	if err != nil {
		return err
	}
	if !ok {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, message)
	}
	savings, err := s.repo.GetSavingsByCode(ctx, tenantID, savingsCode)
	if err != nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "savings not found: "+savingsCode)
	}
	if dataVersion != "" && strconv.FormatInt(savings.DataVersion, 10) != dataVersion {
		return ardaerrors.New(ardaerrors.CodeConflict,
			fmt.Sprintf("dossier changed: savings %s is at version %d", savingsCode, savings.DataVersion))
	}
	if txnDate == "" {
		txnDate = todayDep(ctx)
	}
	if idemKey == "" {
		idemKey = idempotencyKey("dpm-additional", savingsCode)
	}
	entryID, err := s.settlement.post(ctx, tenantID, "DPM_ADDITIONAL", "DPM_OPEN", idemKey,
		savings.SavingsCode, savings.CustomerCode, txnDate, savings.CurrencyCode, amountMinor,
		"CASH_SETTLEMENT_ACCOUNT", "DPM_DEPOSIT_LIABILITY")
	if err != nil {
		return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "additional deposit posting failed", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	defer func() { _ = tx.Rollback() }()
	// Insert the movement first, guarded by the idempotency key: a retry after
	// a committed attempt conflicts here and skips the principal bump instead
	// of crediting the savings twice.
	var txnID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO dpm_transactions
			(id, tenant_id, savings_id, txn_type, amount_minor, currency_code, txn_date, status, journal_entry_id, created_by, idempotency_key)
		VALUES ($1,$2,$3,'TOP_UP_POSTED',$4,$5,$6,'POSTED',$7,$8,$9)
		ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING id`,
		repository.NewDepositID("dpmtx"), tenantID, savings.ID, amountMinor,
		savings.CurrencyCode, txnDate, entryID, actor, idemKey).Scan(&txnID)
	if errors.Is(err, sql.ErrNoRows) {
		// Replay of an already-applied movement: nothing left to do.
		return nil
	}
	if err != nil {
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE dpm_savings SET principal_minor = principal_minor + $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE'
		  AND ($4 = '' OR version::text = $4)`, tenantID, savings.ID, amountMinor, dataVersion)
	if err != nil {
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		// Distinguish a status change from a concurrent write so the operator
		// sees why the approval was refused.
		if dataVersion != "" {
			if current, getErr := s.repo.GetSavingsByCode(ctx, tenantID, savingsCode); getErr == nil &&
				current.Status == "ACTIVE" {
				return ardaerrors.New(ardaerrors.CodeConflict,
					fmt.Sprintf("dossier changed: savings %s is at version %d", savingsCode, current.DataVersion))
			}
		}
		return ardaerrors.New(ardaerrors.CodeConflict, "savings is no longer ACTIVE")
	}
	if err := tx.Commit(); err != nil {
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return nil
}
