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
	"github.com/shopspring/decimal"
)

// ProvisionService runs the monthly general-provision batch (LNM.307.01):
// required provision per agreement = outstanding × rate(debt group) using
// the CM130 evidence rates (0/5/20/50/100); posts the delta (trích tăng
// hoặc hoàn giảm) through the finance PostingService.
type ProvisionService struct {
	repo    *repository.LoanRepository
	db      *sql.DB
	finance batchPostingFinance
}

func NewProvisionService(repo *repository.LoanRepository, db *sql.DB, finance *financeclient.Client) *ProvisionService {
	var batchFinance batchPostingFinance
	if finance != nil {
		batchFinance = finance
	}
	return &ProvisionService{repo: repo, db: db, finance: batchFinance}
}

// RunResult summarizes one provision batch.
type ProvisionRunResult struct {
	ToDate     string   `json:"to_date"`
	Processed  int      `json:"processed"`
	Skipped    int      `json:"skipped"`
	NetMinor   int64    `json:"net_delta_minor"`
	EntryIDs   []string `json:"journal_entry_ids,omitempty"`
	FailedAgmt []string `json:"failed,omitempty"`
}

// Run posts provision deltas for every ACTIVE agreement. Idempotent per
// agreement+date (lnm_provisions unique). Accumulated provision per
// agreement = the sum of prior deltas.
func (s *ProvisionService) Run(ctx context.Context, tenantID, toDate, actor string) (*ProvisionRunResult, error) {
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
	result := &ProvisionRunResult{ToDate: toDate}
	for _, a := range agreements {
		pendingRows, err := s.pendingProvisions(ctx, tenantID, a.AgreementCode, toDate)
		if err != nil {
			result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": load pending provision: "+err.Error())
			continue
		}
		pendingFailed := false
		for _, pending := range pendingRows {
			entryID, err := s.completeProvision(ctx, tenantID, a, pending)
			if err != nil {
				result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": "+err.Error())
				slog.Error("provision retry failed", "agreement", a.AgreementCode, "err", err)
				pendingFailed = true
				break
			}
			result.Processed++
			result.NetMinor += pending.deltaMinor
			result.EntryIDs = append(result.EntryIDs, entryID)
		}
		if pendingFailed {
			continue
		}
		exists, err := s.hasPostedProvisionDate(ctx, tenantID, a.AgreementCode, toDate)
		if err != nil {
			result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": check provision date: "+err.Error())
			continue
		}
		if exists {
			result.Skipped++
			continue
		}
		rate, ok := domain.DebtGroupProvisionRate(a.DebtGroupCode)
		if !ok {
			result.Skipped++
			continue
		}
		requiredMinor, err := domain.RequiredProvisionMinor(a.OutstandingAmt, rate, "VND")
		if err != nil {
			result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": convert required provision: "+err.Error())
			continue
		}
		accumulated, err := s.accumulatedProvision(ctx, tenantID, a.AgreementCode)
		if err != nil {
			result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": load accumulated provision: "+err.Error())
			continue
		}
		delta := domain.ProvisionDelta(requiredMinor, accumulated)
		if delta == 0 {
			result.Skipped++
			continue
		}

		pending, created, err := s.createPendingProvision(ctx, tenantID, a, toDate, rate, requiredMinor, delta, actor)
		if err != nil {
			result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": stage provision: "+err.Error())
			continue
		}
		if !created {
			result.Skipped++
			continue
		}
		entryID, err := s.completeProvision(ctx, tenantID, a, pending)
		if err != nil {
			result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": "+err.Error())
			slog.Error("provision post failed", "agreement", a.AgreementCode, "err", err)
			continue
		}
		result.Processed++
		result.NetMinor += delta
		result.EntryIDs = append(result.EntryIDs, entryID)
	}
	return result, nil
}

type pendingProvision struct {
	id            string
	debtGroupCode string
	provisionDate string
	outstanding   int64
	ratePercent   decimal.Decimal
	requiredMinor int64
	deltaMinor    int64
	journalEntry  sql.NullString
}

func (s *ProvisionService) pendingProvisions(ctx context.Context, tenantID, agreementCode, toDate string) ([]pendingProvision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, debt_group_code, provision_date::text, outstanding_minor,
		       rate_percent::text, required_minor, delta_minor, journal_entry_id::text
		FROM lnm_provisions
		WHERE tenant_id = $1 AND agreement_code = $2 AND status = 'PENDING' AND provision_date <= $3::date
		ORDER BY provision_date, created_at`, tenantID, agreementCode, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []pendingProvision
	for rows.Next() {
		var item pendingProvision
		var rate string
		if err := rows.Scan(&item.id, &item.debtGroupCode, &item.provisionDate, &item.outstanding, &rate, &item.requiredMinor, &item.deltaMinor, &item.journalEntry); err != nil {
			return nil, err
		}
		parsed, err := decimal.NewFromString(rate)
		if err != nil {
			return nil, err
		}
		item.ratePercent = parsed
		pending = append(pending, item)
	}
	return pending, rows.Err()
}

func (s *ProvisionService) createPendingProvision(ctx context.Context, tenantID string, agreement repository.AccruableAgreement, date string, rate decimal.Decimal, required, delta int64, actor string) (pendingProvision, bool, error) {
	var item pendingProvision
	var rateString string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO lnm_provisions
			(tenant_id, agreement_code, debt_group_code, provision_date, outstanding_minor,
			 rate_percent, required_minor, delta_minor, created_by, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'PENDING')
		ON CONFLICT (tenant_id, agreement_code, provision_date) DO NOTHING
		RETURNING id::text, debt_group_code, provision_date::text, outstanding_minor,
		          rate_percent::text, required_minor, delta_minor, journal_entry_id::text`,
		tenantID, agreement.AgreementCode, agreement.DebtGroupCode, date, agreement.OutstandingAmt,
		rate, required, delta, actor).Scan(&item.id, &item.debtGroupCode, &item.provisionDate, &item.outstanding, &rateString, &item.requiredMinor, &item.deltaMinor, &item.journalEntry)
	if err == sql.ErrNoRows {
		return pendingProvision{}, false, nil
	}
	if err != nil {
		return pendingProvision{}, false, err
	}
	item.ratePercent, err = decimal.NewFromString(rateString)
	return item, true, err
}

func (s *ProvisionService) completeProvision(ctx context.Context, tenantID string, agreement repository.AccruableAgreement, pending pendingProvision) (string, error) {
	entryID := pending.journalEntry.String
	if !pending.journalEntry.Valid || entryID == "" {
		postReq := &financev1.PostingRequest{
			IdempotencyKey:    fmt.Sprintf("lnm-provision-%s", pending.id),
			AccountingDate:    pending.provisionDate,
			CurrencyCode:      "VND",
			Description:       fmt.Sprintf("Trích lập dự phòng %s (nhóm %s)", agreement.AgreementCode, pending.debtGroupCode),
			BusinessReference: &financev1.BusinessReference{Domain: "lnm", DocumentType: "LNM_PROVISION", DocumentId: agreement.ID, DocumentCode: agreement.AgreementCode},
			Lines:             s.provisionLines(ctx, agreement, pending.deltaMinor),
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
		UPDATE lnm_provisions SET status = 'POSTED', journal_entry_id = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'PENDING'`, tenantID, pending.id, entryID)
	if err != nil {
		return "", fmt.Errorf("complete provision %s: %w", agreement.AgreementCode, err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if changed == 0 {
		var status string
		if err := s.db.QueryRowContext(ctx, `SELECT status FROM lnm_provisions WHERE tenant_id = $1 AND id = $2`, tenantID, pending.id).Scan(&status); err != nil {
			return "", err
		}
		if status != "POSTED" {
			return "", fmt.Errorf("provision %s is %s after finance post", agreement.AgreementCode, status)
		}
	}
	return entryID, nil
}

func (s *ProvisionService) hasPostedProvisionDate(ctx context.Context, tenantID, agreementCode, date string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM lnm_provisions
			WHERE tenant_id = $1 AND agreement_code = $2 AND provision_date = $3::date AND status = 'POSTED'
		)`, tenantID, agreementCode, date).Scan(&exists)
	return exists, err
}

// accumulatedProvision sums prior provision deltas for the agreement.
func (s *ProvisionService) accumulatedProvision(ctx context.Context, tenantID, agreementCode string) (int64, error) {
	var accum sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(delta_minor), 0) FROM lnm_provisions
		WHERE tenant_id = $1 AND agreement_code = $2 AND status = 'POSTED'`, tenantID, agreementCode).Scan(&accum)
	if err != nil {
		return 0, err
	}
	return accum.Int64, nil
}

// provisionLines builds the 2-line entry: trích tăng (DR expense / CR
// liability, card lines 1-2) hoặc hoàn giảm (DR liability / CR release
// income, card lines 3-4). Rule-card driven (iteration 12): the LNM_PROVISION
// card seeded by 20260909100000 drives the classification; the pre-rules
// hardcoded strings stay as the per-leg fallback so an unseeded/unreachable
// card never breaks the batch. Analytics per leg keep the provision scope.
func (s *ProvisionService) provisionLines(ctx context.Context, a repository.AccruableAgreement, delta int64) []*financev1.PostingLine {
	amount := delta
	cardLine, debitFallback, creditFallback := int32(1), "LNM_PROVISION_EXPENSE", "LNM_PROVISION_LIABILITY"
	if delta < 0 {
		amount = -delta
		cardLine, debitFallback, creditFallback = 3, "LNM_PROVISION_LIABILITY", "LNM_PROVISION_RELEASE"
	}
	analytics := func() *financev1.Analytics {
		return &financev1.Analytics{
			DebtGroupCode: a.DebtGroupCode,
			OrgUnitCode:   a.AccClassification,
			ContractCode:  a.ContractCode,
			Dimensions:    map[string]string{"agreement_code": a.AgreementCode},
		}
	}
	return financeclient.PostingLinesFromRules(
		batchPostingRules(ctx, s.finance, "LNM_PROVISION"),
		[]financeclient.PostingLeg{
			{CardLine: cardLine, Fallback: debitFallback, Direction: "DEBIT", AmountMinor: amount, Analytics: analytics()},
			{CardLine: cardLine + 1, Fallback: creditFallback, Direction: "CREDIT", AmountMinor: amount, Analytics: analytics()},
		},
		"VND",
	)
}
