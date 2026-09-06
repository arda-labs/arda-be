package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/shopspring/decimal"
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// AccrualService computes and posts monthly interest accruals as an EOD
// batch (LNM.301.01): per ACTIVE agreement, interest from the last accrual
// date to the run date via ardamoney.MonthlyInterest, then one LNM_ACCRUAL
// journal entry per agreement through the finance PostingService.
type AccrualService struct {
	repo     *repository.LoanRepository
	db       *sql.DB
	finance  *financeclient.Client
}

func NewAccrualService(repo *repository.LoanRepository, db *sql.DB, finance *financeclient.Client) *AccrualService {
	return &AccrualService{repo: repo, db: db, finance: finance}
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
		lastDate, err := s.lastAccrualDate(ctx, tenantID, a.AgreementCode)
		if err != nil {
			return nil, err
		}
		fromDate := lastDate
		if fromDate == "" {
			fromDate = a.DisburseDate
		}
		days, err := daysBetween(fromDate, toDate)
		if err != nil || days <= 0 {
			result.Skipped++
			continue
		}
		currency := a.CurrencyCode
		if currency == "" {
			currency = "VND"
		}
		// Daily-prorated monthly interest: principal * rate/100 * days/30,
		// rounded to the currency minor unit by ardamoney.
		proratedRate := decimal.NewFromFloat(a.InterestRate * float64(days) / 30.0)
		interest := ardamoney.MonthlyInterest(
			ardamoney.FromMinor(a.OutstandingAmt, currency),
			proratedRate,
			currency,
		)
		if interest.IsZero() {
			result.Skipped++
			continue
		}
		interestMinor, err := ardamoney.ToMinor(interest, currency)
		if err != nil {
			return nil, err
		}

		postReq := &financev1.PostingRequest{
			IdempotencyKey: fmt.Sprintf("lnm-accrual-%s-%s", a.AgreementCode, toDate),
			AccountingDate: toDate,
			CurrencyCode:   currency,
			Description:    fmt.Sprintf("Tính lãi %s %s→%s", a.AgreementCode, fromDate, toDate),
			BusinessReference: &financev1.BusinessReference{
				Domain:       "lnm",
				DocumentType: "LNM_ACCRUAL",
				DocumentId:   a.ID,
				DocumentCode: a.AgreementCode,
			},
			Lines: []*financev1.PostingLine{
				{
					LineNo:       1,
					Direction:    "DEBIT",
					AmountMinor:  interestMinor,
					CurrencyCode: currency,
					Analytics: &financev1.Analytics{
						AccClassification: "LNM_INTEREST_RECEIVABLE",
						DebtGroupCode:     a.DebtGroupCode,
						OrgUnitCode:       a.AccClassification,
						ContractCode:      a.ContractCode,
						Dimensions:        map[string]string{"agreement_code": a.AgreementCode},
					},
					Description: "Phải thu lãi cho vay",
				},
				{
					LineNo:       2,
					Direction:    "CREDIT",
					AmountMinor:  interestMinor,
					CurrencyCode: currency,
					Analytics: &financev1.Analytics{
						AccClassification: "LNM_INTEREST_INCOME",
						OrgUnitCode:       a.AccClassification,
						ContractCode:      a.ContractCode,
						Dimensions:        map[string]string{"agreement_code": a.AgreementCode},
					},
					Description: "Doanh thu lãi cho vay",
				},
			},
		}
		posted, err := s.finance.Post(ctx, postReq)
		if err != nil {
			result.FailedDetail = append(result.FailedDetail, a.AgreementCode+": "+err.Error())
			slog.Error("accrual post failed", "agreement", a.AgreementCode, "err", err)
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO lnm_accruals (tenant_id, agreement_code, from_date, to_date, interest_minor, currency_code, journal_entry_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			tenantID, a.AgreementCode, fromDate, toDate, interestMinor, currency, posted.GetJournalEntryId(), actor); err != nil {
			return nil, fmt.Errorf("record accrual %s: %w", a.AgreementCode, err)
		}
		result.Processed++
		result.TotalMinor += interestMinor
		result.EntryIDs = append(result.EntryIDs, posted.GetJournalEntryId())
	}
	return result, nil
}

// lastAccrualDate returns the latest accrual to_date for the agreement.
func (s *AccrualService) lastAccrualDate(ctx context.Context, tenantID, agreementCode string) (string, error) {
	var last sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(to_date::text) FROM lnm_accruals
		WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, agreementCode).Scan(&last)
	if err != nil {
		return "", err
	}
	if !last.Valid {
		return "", nil
	}
	return last.String, nil
}

// ListAccruals returns recent accrual rows for the loan UI.
func (s *AccrualService) ListAccruals(ctx context.Context, tenantID string, limit int) ([]domain.Accrual, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agreement_code, from_date::text, to_date::text,
		       interest_minor, currency_code, journal_entry_id::text, created_by, created_at
		FROM lnm_accruals WHERE tenant_id = $1
		ORDER BY to_date DESC, agreement_code LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Accrual{}
	for rows.Next() {
		var a domain.Accrual
		var entry sql.NullString
		if err := rows.Scan(&a.ID, &a.TenantID, &a.AgreementCode, &a.FromDate, &a.ToDate,
			&a.InterestMinor, &a.CurrencyCode, &entry, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		if entry.Valid {
			a.JournalEntryID = &entry.String
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// daysBetween counts whole days from a to b (both YYYY-MM-DD).
func daysBetween(fromDate, toDate string) (int, error) {
	from, err := time.Parse("2006-01-02", fromDate)
	if err != nil {
		return 0, fmt.Errorf("invalid from_date %q: %w", fromDate, err)
	}
	to, err := time.Parse("2006-01-02", toDate)
	if err != nil {
		return 0, fmt.Errorf("invalid to_date %q: %w", toDate, err)
	}
	return int(to.Sub(from).Hours() / 24), nil
}
