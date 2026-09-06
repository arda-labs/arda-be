package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
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
	finance *financeclient.Client
}

func NewProvisionService(repo *repository.LoanRepository, db *sql.DB, finance *financeclient.Client) *ProvisionService {
	return &ProvisionService{repo: repo, db: db, finance: finance}
}

// debtGroupRate is the CM130 evidence rate table (TT 02/2023).
var debtGroupRate = map[string]string{
	"GROUP_1": "0",
	"GROUP_2": "5",
	"GROUP_3": "20",
	"GROUP_4": "50",
	"GROUP_5": "100",
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
		rateStr, ok := debtGroupRate[a.DebtGroupCode]
		if !ok {
			result.Skipped++
			continue
		}
		rate := decimal.RequireFromString(rateStr)
		outstanding := ardamoney.FromMinor(a.OutstandingAmt, "VND")
		required := outstanding.Mul(rate).Div(decimal.NewFromInt(100)).Round(0)
		requiredMinor, err := ardamoney.ToMinor(required, "VND")
		if err != nil {
			return nil, err
		}
		accumulated, err := s.accumulatedProvision(ctx, tenantID, a.AgreementCode)
		if err != nil {
			return nil, err
		}
		delta := requiredMinor - accumulated
		if delta == 0 {
			result.Skipped++
			continue
		}

		lines := s.provisionLines(a, delta, requiredMinor)
		postReq := &financev1.PostingRequest{
			IdempotencyKey: fmt.Sprintf("lnm-provision-%s-%s", a.AgreementCode, toDate),
			AccountingDate: toDate,
			CurrencyCode:   "VND",
			Description:    fmt.Sprintf("Trích lập dự phòng %s (nhóm %s)", a.AgreementCode, a.DebtGroupCode),
			BusinessReference: &financev1.BusinessReference{
				Domain:       "lnm",
				DocumentType: "LNM_PROVISION",
				DocumentId:   a.ID,
				DocumentCode: a.AgreementCode,
			},
			Lines: lines,
		}
		posted, err := s.finance.Post(ctx, postReq)
		if err != nil {
			result.FailedAgmt = append(result.FailedAgmt, a.AgreementCode+": "+err.Error())
			slog.Error("provision post failed", "agreement", a.AgreementCode, "err", err)
			continue
		}
		sign := 1
		if delta < 0 {
			sign = -1
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO lnm_provisions
				(tenant_id, agreement_code, debt_group_code, provision_date, outstanding_minor,
				 rate_percent, required_minor, delta_minor, journal_entry_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			tenantID, a.AgreementCode, a.DebtGroupCode, toDate, a.OutstandingAmt,
			rate, requiredMinor, delta*int64(sign), posted.GetJournalEntryId(), actor); err != nil {
			return nil, fmt.Errorf("record provision %s: %w", a.AgreementCode, err)
		}
		result.Processed++
		result.NetMinor += delta
		result.EntryIDs = append(result.EntryIDs, posted.GetJournalEntryId())
	}
	return result, nil
}

// accumulatedProvision sums prior provision deltas for the agreement.
func (s *ProvisionService) accumulatedProvision(ctx context.Context, tenantID, agreementCode string) (int64, error) {
	var accum sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(delta_minor), 0) FROM lnm_provisions
		WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, agreementCode).Scan(&accum)
	if err != nil {
		return 0, err
	}
	return accum.Int64, nil
}

// provisionLines builds the 2-line entry: trích tăng (DR expense / CR
// liability) hoặc hoàn giảm (DR liability / CR release income).
func (s *ProvisionService) provisionLines(a repository.AccruableAgreement, delta, requiredMinor int64) []*financev1.PostingLine {
	amount := delta
	debitClass, creditClass := "LNM_PROVISION_EXPENSE", "LNM_PROVISION_LIABILITY"
	if delta < 0 {
		amount = -delta
		debitClass, creditClass = "LNM_PROVISION_LIABILITY", "LNM_PROVISION_RELEASE"
	}
	base := func(class, direction string, lineNo int32) *financev1.PostingLine {
		return &financev1.PostingLine{
			LineNo:       lineNo,
			Direction:    direction,
			AmountMinor:  amount,
			CurrencyCode: "VND",
			Analytics: &financev1.Analytics{
				AccClassification: class,
				DebtGroupCode:     a.DebtGroupCode,
				OrgUnitCode:       a.AccClassification,
				ContractCode:      a.ContractCode,
				Dimensions:        map[string]string{"agreement_code": a.AgreementCode},
			},
		}
	}
	return []*financev1.PostingLine{
		base(debitClass, "DEBIT", 1),
		base(creditClass, "CREDIT", 2),
	}
}
