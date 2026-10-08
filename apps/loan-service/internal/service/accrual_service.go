package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// AccrualService computes and posts monthly interest accruals as an EOD
// batch (LNM.301.01): per ACTIVE agreement, interest from the last accrual
// date to the run date via ardamoney.MonthlyInterest, then one LNM_ACCRUAL
// journal entry per agreement through the finance PostingService.
type AccrualService struct {
	repo    *repository.LoanRepository
	db      *sql.DB
	finance batchPostingFinance
}

func NewAccrualService(repo *repository.LoanRepository, db *sql.DB, finance *financeclient.Client) *AccrualService {
	var batchFinance batchPostingFinance
	if finance != nil {
		batchFinance = finance
	}
	return &AccrualService{repo: repo, db: db, finance: batchFinance}
}

// RunResult summarizes one accrual batch.
type RunResult struct {
	ToDate       string   `json:"to_date"`
	Processed    int      `json:"processed"`
	Skipped      int      `json:"skipped"`
	TotalMinor   int64    `json:"total_interest_minor"`
	EntryIDs     []string `json:"journal_entry_ids,omitempty"`
	FailedDetail []string `json:"failed,omitempty"`
}

// RunDaily posts accruals for every ACTIVE agreement not yet accrued to
// toDate. Called by the EOD job (HTTP internal endpoint, idempotent per
// agreement+date).
func (s *AccrualService) RunDaily(ctx context.Context, tenantID, toDate, actor string) (*RunResult, error) {
	if s.finance == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "finance client is not configured")
	}
	if !isValidISODate(toDate) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "to_date must be YYYY-MM-DD")
	}

	agreements, err := s.repo.ListActiveAgreementsForAccrual(ctx, tenantID, toDate)
	if err != nil {
		return nil, mapRepoError(err)
	}
	result := &RunResult{ToDate: toDate}
	for _, a := range agreements {
		pendingRows, err := s.pendingAccruals(ctx, tenantID, a.AgreementCode, toDate)
		if err != nil {
			result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": load pending accrual: "+err.Error())
			continue
		}
		pendingFailed := false
		for _, pending := range pendingRows {
			entryID, err := s.completeAccrual(ctx, tenantID, a, pending)
			if err != nil {
				result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": "+err.Error())
				slog.Error("accrual retry failed", "agreement", a.AgreementCode, "err", err)
				pendingFailed = true
				break
			}
			result.Processed++
			result.TotalMinor += pending.interestMinor
			result.EntryIDs = append(result.EntryIDs, entryID)
		}
		if pendingFailed {
			continue
		}

		lastDate, err := s.lastAccrualDate(ctx, tenantID, a.AgreementCode)
		if err != nil {
			result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": load last accrual date: "+err.Error())
			continue
		}
		fromDate := lastDate
		if fromDate == "" {
			fromDate = a.DisburseDate
		}
		days, err := domain.DaysBetween(fromDate, toDate)
		if err != nil || days <= 0 {
			if err != nil {
				result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": "+err.Error())
				continue
			}
			result.Skipped++
			continue
		}
		currency := a.CurrencyCode
		if currency == "" {
			currency = "VND"
		}
		interestMinor, err := domain.AccruedInterestMinor(a.OutstandingAmt, a.InterestRate, days, currency)
		if err != nil {
			result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": convert interest: "+err.Error())
			continue
		}
		if interestMinor == 0 {
			result.Skipped++
			continue
		}
		pending, created, err := s.createPendingAccrual(ctx, tenantID, a.AgreementCode, fromDate, toDate, interestMinor, currency, actor)
		if err != nil {
			result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": stage accrual: "+err.Error())
			continue
		}
		if !created {
			result.Skipped++
			continue
		}
		entryID, err := s.completeAccrual(ctx, tenantID, a, pending)
		if err != nil {
			result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": "+err.Error())
			slog.Error("accrual post failed", "agreement", a.AgreementCode, "err", err)
			continue
		}
		result.Processed++
		result.TotalMinor += interestMinor
		result.EntryIDs = append(result.EntryIDs, entryID)
	}
	return result, nil
}

type pendingAccrual struct {
	id             string
	fromDate       string
	toDate         string
	interestMinor  int64
	currency       string
	journalEntryID sql.NullString
}

func (s *AccrualService) pendingAccruals(ctx context.Context, tenantID, agreementCode, toDate string) ([]pendingAccrual, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, from_date::text, to_date::text, interest_minor, currency_code, journal_entry_id::text
		FROM lnm_accruals
		WHERE tenant_id = $1 AND agreement_code = $2 AND status = 'PENDING' AND to_date <= $3::date
		ORDER BY to_date, created_at`, tenantID, agreementCode, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []pendingAccrual
	for rows.Next() {
		var item pendingAccrual
		if err := rows.Scan(&item.id, &item.fromDate, &item.toDate, &item.interestMinor, &item.currency, &item.journalEntryID); err != nil {
			return nil, err
		}
		pending = append(pending, item)
	}
	return pending, rows.Err()
}

func (s *AccrualService) createPendingAccrual(ctx context.Context, tenantID, agreementCode, fromDate, toDate string, amount int64, currency, actor string) (pendingAccrual, bool, error) {
	var item pendingAccrual
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO lnm_accruals (tenant_id, agreement_code, from_date, to_date, interest_minor, currency_code, created_by, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'PENDING')
		ON CONFLICT (tenant_id, agreement_code, to_date) DO NOTHING
		RETURNING id::text, from_date::text, to_date::text, interest_minor, currency_code, journal_entry_id::text`,
		tenantID, agreementCode, fromDate, toDate, amount, currency, actor).
		Scan(&item.id, &item.fromDate, &item.toDate, &item.interestMinor, &item.currency, &item.journalEntryID)
	if err == sql.ErrNoRows {
		return pendingAccrual{}, false, nil
	}
	if err != nil {
		return pendingAccrual{}, false, err
	}
	return item, true, nil
}

func (s *AccrualService) completeAccrual(ctx context.Context, tenantID string, agreement repository.AccruableAgreement, pending pendingAccrual) (string, error) {
	entryID := pending.journalEntryID.String
	if !pending.journalEntryID.Valid || entryID == "" {
		currency := pending.currency
		if currency == "" {
			currency = "VND"
		}
		lines := financeclient.PostingLinesFromRules(
			batchPostingRules(ctx, s.finance, "LNM_ACCRUAL"),
			[]financeclient.PostingLeg{
				{CardLine: 1, Fallback: "LNM_INTEREST_RECEIVABLE", Direction: "DEBIT", AmountMinor: pending.interestMinor,
					Analytics: &financev1.Analytics{DebtGroupCode: agreement.DebtGroupCode, OrgUnitCode: agreement.AccClassification, ContractCode: agreement.ContractCode, Dimensions: map[string]string{"agreement_code": agreement.AgreementCode}}, Description: "Phải thu lãi cho vay"},
				{CardLine: 2, Fallback: "LNM_INTEREST_INCOME", Direction: "CREDIT", AmountMinor: pending.interestMinor,
					Analytics: &financev1.Analytics{OrgUnitCode: agreement.AccClassification, ContractCode: agreement.ContractCode, Dimensions: map[string]string{"agreement_code": agreement.AgreementCode}}, Description: "Doanh thu lãi cho vay"},
			}, currency)
		postReq := &financev1.PostingRequest{
			IdempotencyKey:    fmt.Sprintf("lnm-accrual-%s", pending.id),
			AccountingDate:    pending.toDate,
			CurrencyCode:      currency,
			Description:       fmt.Sprintf("Tính lãi %s %s→%s", agreement.AgreementCode, pending.fromDate, pending.toDate),
			BusinessReference: &financev1.BusinessReference{Domain: "lnm", DocumentType: "LNM_ACCRUAL", DocumentId: agreement.ID, DocumentCode: agreement.AgreementCode},
			Lines:             lines,
		}
		posted, err := s.finance.Post(ctx, postReq)
		if err != nil {
			return "", err
		}
		entryID = posted.GetJournalEntryId()
		if entryID == "" {
			return "", fmt.Errorf("finance returned an empty journal entry id")
		}
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE lnm_accruals SET status = 'POSTED', journal_entry_id = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'PENDING'`, tenantID, pending.id, entryID)
	if err != nil {
		return "", fmt.Errorf("complete accrual %s: %w", agreement.AgreementCode, err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if changed == 0 {
		var status string
		if err := s.db.QueryRowContext(ctx, `SELECT status FROM lnm_accruals WHERE tenant_id = $1 AND id = $2`, tenantID, pending.id).Scan(&status); err != nil {
			return "", err
		}
		if status != "POSTED" {
			return "", fmt.Errorf("accrual %s is %s after finance post", agreement.AgreementCode, status)
		}
	}
	return entryID, nil
}

// lastAccrualDate returns the latest accrual to_date for the agreement.
func (s *AccrualService) lastAccrualDate(ctx context.Context, tenantID, agreementCode string) (string, error) {
	var last sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(to_date::text) FROM lnm_accruals
		WHERE tenant_id = $1 AND agreement_code = $2 AND status = 'POSTED'`, tenantID, agreementCode).Scan(&last)
	if err != nil {
		return "", err
	}
	if !last.Valid {
		return "", nil
	}
	return last.String, nil
}

// ListAccruals returns one page of accrual rows for the loan UI. q matches
// the agreement code (ILIKE); sort/order are whitelisted by the handler's
// ListSpec (agreement_code, accrual_date, created_at).
func (s *AccrualService) ListAccruals(ctx context.Context, tenantID, q, sort, order string, page, perPage int) ([]domain.Accrual, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	sortCol := "created_at"
	switch sort {
	case "agreement_code":
		sortCol = "agreement_code"
	case "accrual_date":
		sortCol = "to_date"
	}
	direction := "DESC"
	if sort != "" && order != "desc" {
		direction = "ASC"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agreement_code, from_date::text, to_date::text,
		       interest_minor, currency_code, journal_entry_id::text, created_by, created_at,
		       count(*) OVER() AS total_count
		FROM lnm_accruals
		WHERE tenant_id = $1
		  AND ($2::text = '' OR agreement_code ILIKE '%' || $2::text || '%')
		ORDER BY `+sortCol+` `+direction+`, id
		LIMIT $3::int OFFSET $4::int`, tenantID, q, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.Accrual{}
	total := 0
	for rows.Next() {
		var a domain.Accrual
		var entry sql.NullString
		if err := rows.Scan(&a.ID, &a.TenantID, &a.AgreementCode, &a.FromDate, &a.ToDate,
			&a.InterestMinor, &a.CurrencyCode, &entry, &a.CreatedBy, &a.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		if entry.Valid {
			a.JournalEntryID = &entry.String
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
