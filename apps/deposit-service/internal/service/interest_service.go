package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/interest"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/shopspring/decimal"
)

// DPM interest case types (EPAS DPM.100/101 rates; 302/303/304 operations).
const (
	CaseRateRegister  = "DPM_RATE_REGISTER_V1"
	CaseRateEdit      = "DPM_RATE_EDIT_V1"
	CasePayInterest   = "DPM_PAY_INTEREST_V1"
	CaseCapitalize    = "DPM_CAPITALIZE_V1"
	CaseBatchInterest = "DPM_BATCH_INTEREST_V1"
)

// SavingsDetail is the aggregate read model for the savings detail screen.
type SavingsDetail struct {
	Savings  *repository.Savings     `json:"savings"`
	Txns     []repository.DepositTxn `json:"transactions"`
	Accruals []repository.Accrual    `json:"accruals"`
	Interest []repository.InterestOp `json:"interest_ops"`
}

// InterestService runs DPM rate tiers, daily accrual (DPM.305) and
// pay/capitalize operations (DPM.302/303/304).
type InterestService struct {
	repo     *repository.DepositRepository
	db       *sql.DB
	finance  *financeclient.Client
	workflow WorkflowSubmitter
	// accruals is the storage RunDaily needs; it is the repository, and a fake
	// in the unit tests of the daily accrual.
	accruals accrualStore
	// postGL posts one interest GL entry; it is postInterest, and a fake in tests.
	postGL func(ctx context.Context, tenantID, card, idemKey, accountingDate string, savings *repository.Savings, amountMinor int64, debitClass, creditClass, description string) (string, error)
}

func NewInterestService(repo *repository.DepositRepository, db *sql.DB, finance *financeclient.Client, workflow WorkflowSubmitter) *InterestService {
	s := &InterestService{repo: repo, db: db, finance: finance, workflow: workflow, accruals: repo}
	s.postGL = s.postInterest
	return s
}

// ── Rate tiers ──

// ListInterestRates returns active tiers (optionally one product).
func (s *InterestService) ListInterestRates(ctx context.Context, tenantID, productCode string) ([]repository.InterestRate, error) {
	return s.repo.ListInterestRates(ctx, tenantID, productCode)
}

type ratePayload struct {
	ProductCode   string  `json:"product_code"`
	TermMonths    int     `json:"term_months"`
	Method        string  `json:"method"`
	Denominator   int     `json:"denominator"`
	Rate          float64 `json:"rate"`
	EffectiveFrom string  `json:"effective_from"`
}

// SubmitRate stages a rate register/edit request case.
func (s *InterestService) SubmitRate(ctx context.Context, tenantID, actor, requestType string, payload json.RawMessage) (*repository.RateRequest, error) {
	requestType = strings.ToUpper(requestType)
	caseType := ""
	switch requestType {
	case "REGISTER", "ADJUST":
		caseType = CaseRateRegister
	case "EDIT":
		caseType = CaseRateEdit
	default:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "request_type must be REGISTER, EDIT or ADJUST")
	}
	var p ratePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "invalid rate payload")
	}
	if p.Rate <= 0 || p.EffectiveFrom == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "rate and effective_from are required")
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	request, err := s.repo.CreateRateRequest(ctx, &repository.RateRequest{
		TenantID:    tenantID,
		RequestType: requestType,
		Payload:     payload,
		Status:      "DRAFT",
		CreatedBy:   actor,
	})
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          caseType,
		Title:             fmt.Sprintf("Đăng ký lãi suất huy động %s", requestType),
		PrimaryObjectType: "dpm.rate_request",
		PrimaryObjectID:   request.ID,
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("dpm-rate-%s", request.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"requestId":   request.ID,
		"requestType": requestType,
	}, fmt.Sprintf("dpm-rate-%s-submit", request.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetRateRequestCase(ctx, tenantID, request.ID, caseCreated.Id); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	request.WorkflowCaseID = &caseCreated.Id
	request.Status = "SUBMITTED"
	return request, nil
}

// CheckRateRequest validates the staged rate request for the case validate step.
func (s *InterestService) CheckRateRequest(ctx context.Context, tenantID, id string) (bool, string, error) {
	request, err := s.repo.GetRateRequestByID(ctx, tenantID, id)
	if err != nil {
		return false, "", err
	}
	if request == nil {
		return false, "rate request not found", nil
	}
	if request.Status != "SUBMITTED" {
		return false, "status " + request.Status + " is not actionable", nil
	}
	return true, "", nil
}

// GetRateRequest returns one staged rate request (checker dossier read;
// includes data_version).
func (s *InterestService) GetRateRequest(ctx context.Context, tenantID, id string) (*repository.RateRequest, error) {
	request, err := s.repo.GetRateRequestByID(ctx, tenantID, id)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return request, nil
}

// GetInterestOpsByCase returns the staged ops of one workflow case — the
// checker dossier read for DPM.302/303/304 (single op or whole batch).
func (s *InterestService) GetInterestOpsByCase(ctx context.Context, tenantID, caseID string) ([]repository.InterestOp, error) {
	ops, err := s.repo.ListInterestOpsByCase(ctx, tenantID, caseID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return ops, nil
}

// ResolveRateRequest applies the checker decision (APPROVE upserts the tier).
func (s *InterestService) ResolveRateRequest(ctx context.Context, tenantID, id, decision, actor string, dataVersion int64) error {
	request, err := s.repo.GetRateRequestByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if request == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "rate request not found")
	}
	switch decision {
	case "APPROVE":
		if request.Status == "APPLIED" {
			return nil
		}
		if request.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "rate request is not SUBMITTED")
		}
		var p ratePayload
		if err := json.Unmarshal(request.Payload, &p); err != nil {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "invalid rate payload")
		}
		if _, err := s.repo.UpsertInterestRate(ctx, &repository.InterestRate{
			TenantID:      tenantID,
			ProductCode:   p.ProductCode,
			TermMonths:    p.TermMonths,
			Method:        p.Method,
			Denominator:   p.Denominator,
			Rate:          p.Rate,
			EffectiveFrom: p.EffectiveFrom,
			CreatedBy:     actor,
		}); err != nil {
			return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		if err := s.repo.SetRateRequestStatus(ctx, tenantID, id, "APPLIED", dataVersion); err != nil {
			return mapRateResolveErr(err)
		}
		return nil
	case "REJECT":
		if request.Status == "REJECTED" {
			return nil
		}
		if request.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "rate request is not SUBMITTED")
		}
		if err := s.repo.SetRateRequestStatus(ctx, tenantID, id, "REJECTED", dataVersion); err != nil {
			return mapRateResolveErr(err)
		}
		return nil
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
}

// mapRateResolveErr surfaces a stale-dossier conflict (Aborted at the gRPC
// boundary) instead of collapsing it into a generic internal error.
func mapRateResolveErr(err error) error {
	if errors.Is(err, repository.ErrDossierChanged) {
		return ardaerrors.Wrap(ardaerrors.CodeConflict, "dossier changed while in review", err)
	}
	return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
}

// ── Accrual (DPM.305, EOD) ──

// accrualWindow returns the half-open date window [from, to) still to accrue
// for one savings account, given the last confirmed period ("" when none), the
// run business date and the start of the current interest period ("" = the
// open date). The run accrues through the business date inclusive, so
// to = business date + 1. Because to is the first day the next run would start
// from, consecutive windows chain without losing or repeating a day
// (G2-DPM-NGAY-01/02). The window never starts before periodStart, so a row
// never straddles two interest periods. It is pure so the retry semantics can
// be unit tested.
func accrualWindow(openDate, lastPosted, businessDate, periodStart string) (from, to string, days int, ok bool) {
	business, err := time.Parse("2006-01-02", businessDate)
	if err != nil {
		return "", "", 0, false
	}
	from = openDate
	if lastPosted != "" {
		last, err := time.Parse("2006-01-02", lastPosted)
		if err != nil {
			return "", "", 0, false
		}
		from = last.AddDate(0, 0, 1).Format("2006-01-02")
	}
	if periodStart != "" && periodStart > from {
		from = periodStart // ISO dates compare as strings
	}
	fromDate, err := time.Parse("2006-01-02", from)
	if err != nil || business.Before(fromDate) {
		return "", "", 0, false
	}
	exclusiveEnd := business.AddDate(0, 0, 1)
	days = int(exclusiveEnd.Sub(fromDate) / (24 * time.Hour))
	if days <= 0 {
		return "", "", 0, false
	}
	return from, exclusiveEnd.Format("2006-01-02"), days, true
}

// RunDaily accrues interest per active savings up to businessDate.
//
// The amount is cumulative per interest period (owner decision of 2026-10-18):
// round(interest from the period start through businessDate, by the product
// ROUND_NO / CM163) minus the total already accrued in the period. The period
// starts at the open date or at the effective date of the last PAY/CAPITALIZE
// op; the opening balance, top-ups, rate history and the total already accrued
// are all read from stored rows, so a rerun of the same day adds nothing. Days
// whose amount is zero or negative write no row; a later day makes them up.
//
// A savings whose product has no ROUND_NO/ROUND_TYPE is skipped and the run
// ends with one error carrying dpm.accrual.round_config_missing and the list of
// skipped savings; the other savings are accrued normally.
//
// Each row is staged PENDING first and only completed by completeAccrual:
// post the GL entry with the stable row id as idempotency key, then flip the
// row POSTED while adding the amount to the savings accrued balance in one
// transaction. LastAccrualPeriod ignores PENDING rows, so a run that fails
// midway leaves the day to be retried (not silently skipped) by the next run,
// and the retry reuses the same GL key so finance de-duplicates the posting.
func (s *InterestService) RunDaily(ctx context.Context, tenantID, businessDate string) (int, error) {
	business, err := time.Parse("2006-01-02", businessDate)
	if err != nil {
		return 0, ardaerrors.New(ardaerrors.CodeInvalidInput, "business_date must be YYYY-MM-DD")
	}
	savings, err := s.accruals.ListActiveSavingsForAccrual(ctx, tenantID)
	if err != nil {
		return 0, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	posted := 0
	var skipped []string
	for i := range savings {
		item := &savings[i]

		// Complete days stranded by an earlier failed run first: the row keeps
		// its id, so the GL idempotency key is stable and a retry cannot create
		// a second journal entry. Finishing them before computing the next
		// window keeps MAX(period_to) — and the accrual range — in order.
		for {
			pending, err := s.accruals.FirstPendingAccrual(ctx, tenantID, item.ID)
			if err != nil {
				return posted, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
			}
			if pending == nil {
				break
			}
			if err := s.completeAccrual(ctx, tenantID, item, pending); err != nil {
				return posted, err
			}
			posted++
		}

		last, err := s.accruals.LastAccrualPeriod(ctx, tenantID, item.ID)
		if err != nil {
			return posted, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		storedStart, err := s.accruals.InterestPeriodStart(ctx, tenantID, item.ID)
		if err != nil {
			return posted, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		from, _, days, ok := accrualWindow(item.OpenDate, last, businessDate, storedStart)
		if !ok {
			continue
		}
		periodStart := item.OpenDate
		if storedStart > periodStart {
			periodStart = storedStart
		}
		input, err := s.cumulativeInput(ctx, tenantID, item, periodStart, business)
		if err != nil {
			return posted, err
		}
		result, err := computeCumulativeAccrual(input)
		if err != nil {
			var typed *ardaerrors.Error
			if errors.As(err, &typed) && typed.Code == CodeAccrualRoundConfigMissing {
				skipped = append(skipped, item.SavingsCode)
				continue
			}
			return posted, err
		}
		if result.Amount <= 0 {
			continue
		}
		accrual := &repository.Accrual{
			TenantID:    tenantID,
			SavingsID:   item.ID,
			SavingsCode: item.SavingsCode,
			PeriodFrom:  from,
			PeriodTo:    businessDate,
			Days:        days,
			BaseMinor:   item.PrincipalMinor,
			Rate:        input.RateOn(business).InexactFloat64(),
			AmountMinor: result.Amount,
		}
		inserted, err := s.accruals.CreateAccrual(ctx, accrual)
		if err != nil {
			return posted, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		if !inserted {
			// A concurrent worker staged this period first; MarkAccrualPosted is
			// guarded by the PENDING → POSTED transition, so only one of them can
			// apply the amount. The survivor is completed by a later run.
			continue
		}
		if err := s.completeAccrual(ctx, tenantID, item, accrual); err != nil {
			return posted, err
		}
		posted++
	}
	if len(skipped) > 0 {
		typed := ardaerrors.New(CodeAccrualRoundConfigMissing, fmt.Sprintf(
			"daily accrual skipped for %d savings whose product has no ROUND_NO/ROUND_TYPE; %d accrued", len(skipped), posted))
		typed.Fields = map[string]string{
			"savings_codes": strings.Join(skipped, ","),
			"posted":        strconv.Itoa(posted),
		}
		return posted, typed
	}
	return posted, nil
}

// cumulativeInput reads everything the cumulative accrual of one savings needs
// from stored rows: the product rounding, the rate on the period start and its
// later changes, the top-ups of the period and the total already accrued.
func (s *InterestService) cumulativeInput(ctx context.Context, tenantID string, item *repository.Savings, periodStart string, business time.Time) (cumulativeAccrualInput, error) {
	start, err := time.Parse("2006-01-02", periodStart)
	if err != nil {
		return cumulativeAccrualInput{}, ardaerrors.New(ardaerrors.CodeInternal, "invalid interest period start "+periodStart)
	}
	businessDate := business.Format("2006-01-02")
	in := cumulativeAccrualInput{
		PeriodStart:      start,
		BusinessDate:     business,
		CurrentPrincipal: decimal.NewFromInt(item.PrincipalMinor),
		Denominator:      365,
	}
	product, err := s.accruals.GetProductByCode(ctx, tenantID, item.ProductCode)
	if err != nil {
		return in, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if product != nil {
		in.RoundNo, in.RoundType = product.RoundNo, product.RoundType
	}
	productRate := decimal.Zero
	if product != nil {
		productRate = decimal.NewFromFloat(product.InterestRate)
	}
	// Rate in force on the period start, then every change up to the business
	// date. The denominator of the row in force on the business date applies.
	rateAt := func(date string) (*repository.InterestRate, error) {
		row, err := s.accruals.FindEffectiveRate(ctx, tenantID, item.ProductCode, 0, date)
		if err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		return row, nil
	}
	startRow, err := rateAt(periodStart)
	if err != nil {
		return in, err
	}
	in.Rate = productRate
	if startRow != nil {
		in.Rate = decimal.NewFromFloat(startRow.Rate)
	}
	changes, err := s.accruals.ListRateChangeDates(ctx, tenantID, item.ProductCode, 0, periodStart, businessDate)
	if err != nil {
		return in, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	for _, date := range changes {
		row, err := rateAt(date)
		if err != nil {
			return in, err
		}
		if row == nil {
			continue
		}
		d, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		in.RatePoints = append(in.RatePoints, interest.RatePoint{Date: d, Rate: decimal.NewFromFloat(row.Rate)})
	}
	if endRow, err := rateAt(businessDate); err != nil {
		return in, err
	} else if endRow != nil && endRow.Denominator > 0 {
		in.Denominator = endRow.Denominator
	}
	topUps, err := s.accruals.ListPostedTopUps(ctx, tenantID, item.ID, periodStart)
	if err != nil {
		return in, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	for _, t := range topUps {
		d, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			continue
		}
		in.TopUps = append(in.TopUps, topUp{Date: d, Amount: decimal.NewFromInt(t.AmountMinor)})
	}
	prior, err := s.accruals.SumPeriodAccruals(ctx, tenantID, item.ID, periodStart)
	if err != nil {
		return in, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	in.PriorAccrued = prior
	return in, nil
}

// completeAccrual finishes one staged accrual: post the GL entry when it is
// still missing, then atomically flip the row POSTED and add the amount to the
// savings accrued balance. The row id keeps the GL idempotency key
// deterministic, so completing a PENDING row twice is safe.
func (s *InterestService) completeAccrual(ctx context.Context, tenantID string, item *repository.Savings, a *repository.Accrual) error {
	entryID := ""
	if a.JournalID != nil {
		entryID = *a.JournalID
	}
	if entryID == "" {
		var err error
		entryID, err = s.postGL(ctx, tenantID, "DPM_ACCRUAL", "dpm-accrual-"+a.ID,
			a.PeriodTo, item, a.AmountMinor, "DPM_INTEREST_EXPENSE", "DPM_INTEREST_PAYABLE", "Dự chi lãi tiền gửi")
		if err != nil {
			return err
		}
	}
	if err := s.accruals.MarkAccrualPosted(ctx, tenantID, a.ID, entryID); err != nil {
		if errors.Is(err, repository.ErrSavingsNotActive) {
			return ardaerrors.New(ardaerrors.CodeConflict, "savings is no longer ACTIVE — accrual cannot be applied")
		}
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return nil
}

// ── Interest operations (DPM.302/303/304) ──

// SubmitInterest stages a PAY/CAPITALIZE op (or a BATCH of PAY ops when
// savingsCode == "" and opType == "BATCH").
func (s *InterestService) SubmitInterest(ctx context.Context, tenantID, actor, savingsCode, opType string, amountMinor int64) (*repository.InterestOp, []repository.InterestOp, error) {
	opType = strings.ToUpper(strings.TrimSpace(opType))
	if s.workflow == nil {
		return nil, nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	switch opType {
	case "PAY", "CAPITALIZE":
		savings, err := s.repo.GetSavingsByCode(ctx, tenantID, savingsCode)
		if err != nil || savings == nil {
			return nil, nil, ardaerrors.New(ardaerrors.CodeNotFound, "savings not found")
		}
		if savings.Status != "ACTIVE" {
			return nil, nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "savings is not ACTIVE")
		}
		if amountMinor <= 0 || amountMinor > savings.AccruedMinor {
			return nil, nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount must be positive and within accrued interest")
		}
		op := &repository.InterestOp{
			TenantID:    tenantID,
			SavingsID:   savings.ID,
			SavingsCode: savings.SavingsCode,
			OpType:      opType,
			AmountMinor: amountMinor,
			Status:      "DRAFT",
			CreatedBy:   actor,
		}
		created, err := s.repo.CreateInterestOp(ctx, op)
		if err != nil {
			return nil, nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		caseType := CasePayInterest
		if opType == "CAPITALIZE" {
			caseType = CaseCapitalize
		}
		caseID, err := s.startInterestCase(ctx, tenantID, actor, caseType, created, nil)
		if err != nil {
			return nil, nil, err
		}
		created.WorkflowCaseID = &caseID
		created.Status = "SUBMITTED"
		return created, nil, nil
	case "BATCH":
		savings, err := s.repo.ListActiveSavingsForAccrual(ctx, tenantID)
		if err != nil {
			return nil, nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		ops := []repository.InterestOp{}
		for i := range savings {
			item := &savings[i]
			if item.AccruedMinor <= 0 {
				continue
			}
			op := &repository.InterestOp{
				TenantID:    tenantID,
				SavingsID:   item.ID,
				SavingsCode: item.SavingsCode,
				OpType:      "PAY",
				AmountMinor: item.AccruedMinor,
				Status:      "DRAFT",
				CreatedBy:   actor,
			}
			created, err := s.repo.CreateInterestOp(ctx, op)
			if err != nil {
				return nil, nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
			}
			ops = append(ops, *created)
		}
		if len(ops) == 0 {
			return nil, nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "no savings with accrued interest")
		}
		ids := make([]string, 0, len(ops))
		for i := range ops {
			ids = append(ids, ops[i].ID)
		}
		caseID, err := s.startInterestBatchCase(ctx, tenantID, actor, ids)
		if err != nil {
			return nil, nil, err
		}
		for i := range ops {
			ops[i].Status = "SUBMITTED"
			ops[i].WorkflowCaseID = &caseID
		}
		return nil, ops, nil
	default:
		return nil, nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "op_type must be PAY, CAPITALIZE or BATCH")
	}
}

func (s *InterestService) startInterestCase(ctx context.Context, tenantID, actor, caseType string, op *repository.InterestOp, batchID *string) (string, error) {
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          caseType,
		Title:             fmt.Sprintf("Trả lãi sổ %s", op.SavingsCode),
		PrimaryObjectType: "dpm.interest_op",
		PrimaryObjectID:   op.ID,
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("dpm-interest-%s", op.ID),
	})
	if err != nil {
		return "", ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"opId":        op.ID,
		"opType":      op.OpType,
		"savingsCode": op.SavingsCode,
	}, fmt.Sprintf("dpm-interest-%s-submit", op.ID)); err != nil {
		return "", ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetInterestOpCase(ctx, tenantID, op.ID, caseCreated.Id); err != nil {
		return "", ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return caseCreated.Id, nil
}

func (s *InterestService) startInterestBatchCase(ctx context.Context, tenantID, actor string, ids []string) (string, error) {
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseBatchInterest,
		Title:             fmt.Sprintf("Trả lãi hàng loạt %d sổ", len(ids)),
		PrimaryObjectType: "dpm.interest_batch",
		PrimaryObjectID:   ids[0],
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("dpm-interest-batch-%s", ids[0]),
	})
	if err != nil {
		return "", ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	// One case processes every op; each op is stamped with the batch case.
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"opIds":  ids,
		"opType": "BATCH",
	}, fmt.Sprintf("dpm-interest-batch-%s-submit", ids[0])); err != nil {
		return "", ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	for _, id := range ids {
		if err := s.repo.SetInterestOpCase(ctx, tenantID, id, caseCreated.Id); err != nil {
			return "", ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
	}
	return caseCreated.Id, nil
}

// CheckInterestOp validates one staged op.
func (s *InterestService) CheckInterestOp(ctx context.Context, tenantID, id string) (bool, string, error) {
	op, err := s.repo.GetInterestOpByID(ctx, tenantID, id)
	if err != nil {
		return false, "", err
	}
	if op == nil {
		return false, "interest op not found", nil
	}
	// POSTING ops already passed the checker and only failed while posting;
	// keep them actionable so a retried case resumes instead of blocking.
	if op.Status != "SUBMITTED" && op.Status != "POSTING" {
		return false, "status " + op.Status + " is not actionable", nil
	}
	return true, "", nil
}

// opMode describes how ResolveInterestOp must treat the op it loaded. The
// mapping is pure so the posting state machine is unit tested without a DB.
type opMode int

const (
	opModeFresh   opMode = iota // SUBMITTED: reserve the accrued amount, then post GL
	opModeResume                // POSTING: amount already reserved, finish the GL side
	opModeSettled               // POSTED: idempotent no-op
	opModeInvalid               // REJECTED / unknown state
)

func resolveOpMode(status string) opMode {
	switch status {
	case "SUBMITTED":
		return opModeFresh
	case "POSTING":
		return opModeResume
	case "POSTED":
		return opModeSettled
	default:
		return opModeInvalid
	}
}

// ResolveInterestOp posts + applies one op (APPROVE) or rejects it.
//
// Ordering decision (money safety): the accrued balance is reserved BEFORE the
// GL posting, as part of the SUBMITTED → POSTING transition. An op that cannot
// reserve its amount never reaches finance, so payouts can never exceed the
// accrued interest (the guarded decrement has no GREATEST() masking), and two
// concurrent approvals cannot both pass. A failed GL post is compensated
// (balance restored, op back to SUBMITTED); when the process dies between the
// two steps the op stays POSTING and the retry resumes at the GL step with the
// same deterministic key (dpm-interest-<op id>), so finance de-duplicates and
// the balance is never decremented twice. The state machine therefore cannot be
// "paid in GL but still carrying accrued interest".
func (s *InterestService) ResolveInterestOp(ctx context.Context, tenantID, id, decision, actor, dataVersion string) error {
	op, err := s.repo.GetInterestOpByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if op == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "interest op not found")
	}
	switch decision {
	case "APPROVE":
		mode := resolveOpMode(op.Status)
		if mode == opModeSettled {
			return nil
		}
		if mode == opModeInvalid {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "interest op status "+op.Status+" is not actionable")
		}
		savings, err := s.repo.GetSavingsByCode(ctx, tenantID, op.SavingsCode)
		if err != nil || savings == nil {
			return ardaerrors.New(ardaerrors.CodeNotFound, "savings not found")
		}
		if dataVersion != "" && strconv.FormatInt(savings.DataVersion, 10) != dataVersion {
			return ardaerrors.New(ardaerrors.CodeConflict,
				fmt.Sprintf("dossier changed: savings %s is at version %d", op.SavingsCode, savings.DataVersion))
		}
		card, debit, credit, description := interestOpCard(op.OpType)
		capitalize := op.OpType == "CAPITALIZE"
		if mode == opModeFresh {
			// The op takes effect on the business date it is approved and starts a
			// new interest period (owner decision: PAY and CAPITALIZE both do).
			if err := s.repo.BeginInterestOpPosting(ctx, tenantID, op.ID, savings.ID, op.AmountMinor, capitalize, dataVersion, todayDep(ctx)); err != nil {
				switch {
				case errors.Is(err, repository.ErrAccruedInsufficient):
					if dataVersion != "" {
						if current, gErr := s.repo.GetSavingsByCode(ctx, tenantID, op.SavingsCode); gErr == nil && current != nil &&
							current.Status == "ACTIVE" &&
							strconv.FormatInt(current.DataVersion, 10) != dataVersion {
							return ardaerrors.New(ardaerrors.CodeConflict,
								fmt.Sprintf("dossier changed: savings %s is at version %d", op.SavingsCode, current.DataVersion))
						}
					}
					return ardaerrors.New(ardaerrors.CodeConflict, "amount exceeds the accrued interest or the savings account is no longer ACTIVE")
				case errors.Is(err, repository.ErrInterestOpState):
					// A concurrent worker advanced the op between our read and
					// the guarded transition; a retry picks up its state.
					return ardaerrors.New(ardaerrors.CodeConflict, "interest op is already being processed")
				default:
					return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
				}
			}
		}
		entryID, err := s.postInterest(ctx, tenantID, card, fmt.Sprintf("dpm-interest-%s", op.ID),
			op.CreatedAt.Format("2006-01-02"), savings, op.AmountMinor, debit, credit, description)
		if err != nil {
			// A concurrent worker may have completed (or compensated) the op
			// while our posting was failing; re-read before reporting a
			// failure so a false negative does not raise a permanent error.
			if cErr := s.repo.CompensateInterestOp(ctx, tenantID, op.ID, savings.ID, op.AmountMinor, capitalize); cErr != nil {
				if cur, gErr := s.repo.GetInterestOpByID(ctx, tenantID, op.ID); gErr == nil && cur != nil && cur.Status == "POSTED" {
					return nil
				}
				return ardaerrors.Wrap(ardaerrors.CodeInternal,
					"interest posting failed and the reserved accrual could not be restored; retry resumes from POSTING", err)
			}
			return err
		}
		if err := s.repo.MarkInterestOpPosted(ctx, tenantID, op.ID, entryID); err != nil {
			return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		return nil
	case "REJECT":
		if op.Status == "REJECTED" {
			return nil
		}
		if op.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "interest op is not SUBMITTED")
		}
		return s.repo.SetInterestOpStatus(ctx, tenantID, op.ID, "REJECTED")
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
}

// GetSavingsDetail returns the savings aggregate for the detail screen.
func (s *InterestService) GetSavingsDetail(ctx context.Context, tenantID, code string) (*SavingsDetail, error) {
	savings, err := s.repo.GetSavingsByCode(ctx, tenantID, code)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if savings == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "savings not found")
	}
	txns, err := s.repo.ListTxnsBySavings(ctx, tenantID, savings.ID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	accruals, err := s.repo.ListAccrualsBySavings(ctx, tenantID, savings.ID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	ops, err := s.repo.ListInterestOpsBySavings(ctx, tenantID, savings.ID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return &SavingsDetail{Savings: savings, Txns: txns, Accruals: accruals, Interest: ops}, nil
}

// interestOpCard maps a PAY/CAPITALIZE op onto its finance card and the
// DR/CR classifications (docs/accounting-rule-cards.md DPM_INTEREST_PAY /
// DPM_CAPITALIZE). Pure so the golden table can lock it.
func interestOpCard(opType string) (card, debitClass, creditClass, description string) {
	if opType == "CAPITALIZE" {
		return "DPM_CAPITALIZE", "DPM_INTEREST_PAYABLE", "DPM_DEPOSIT_LIABILITY", "Lãi nhập gốc tiền gửi"
	}
	return "DPM_INTEREST_PAY", "DPM_INTEREST_PAYABLE", "CASH_SETTLEMENT_ACCOUNT", "Trả lãi tiền gửi"
}

// postInterest posts one 2-leg interest entry.
func (s *InterestService) postInterest(ctx context.Context, tenantID, card, idemKey, accountingDate string, savings *repository.Savings, amountMinor int64, debitClass, creditClass, description string) (string, error) {
	if s.finance == nil {
		return "", ardaerrors.New(ardaerrors.CodeInternal, "finance client is not configured")
	}
	analytics := func(class string) *financev1.Analytics {
		return &financev1.Analytics{
			AccClassification: class,
			OrgUnitCode:       savings.OrgCode,
			Dimensions:        map[string]string{"savings_code": savings.SavingsCode},
		}
	}
	posted, err := s.finance.Post(ctx, &financev1.PostingRequest{
		IdempotencyKey: idemKey,
		AccountingDate: accountingDate,
		CurrencyCode:   savings.CurrencyCode,
		Description:    description,
		BusinessReference: &financev1.BusinessReference{
			Domain:       "dpm",
			DocumentType: card,
			DocumentId:   savings.ID,
			DocumentCode: savings.SavingsCode,
		},
		Lines: []*financev1.PostingLine{
			{LineNo: 1, Direction: "DEBIT", AmountMinor: amountMinor, CurrencyCode: savings.CurrencyCode, Analytics: analytics(debitClass)},
			{LineNo: 2, Direction: "CREDIT", AmountMinor: amountMinor, CurrencyCode: savings.CurrencyCode, Analytics: analytics(creditClass)},
		},
	})
	if err != nil {
		return "", ardaerrors.Wrap(ardaerrors.CodeBadGateway, "interest posting failed", err)
	}
	return posted.GetJournalEntryId(), nil
}
