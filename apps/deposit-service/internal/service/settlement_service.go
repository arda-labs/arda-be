package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// WorkflowSubmitter is the workflow case submission contract (shared by the
// settlement and additional-deposit services).
type WorkflowSubmitter interface {
	CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error)
	SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error)
}

// Submission is the workflow case handle returned to the FE after submit.
type Submission struct {
	CaseID   string `json:"case_id"`
	CaseCode string `json:"case_code"`
}

// SettlementService runs deposit open/settle movements. Posting goes through
// the finance PostingService rule cards DPM_OPEN / DPM_SETTLEMENT
// (docs/accounting-rule-cards.md, seeded by 20260910210000_dpm_rule_cards.sql).
type SettlementService struct {
	repo     *repository.DepositRepository
	db       *sql.DB
	finance  *financeclient.Client
	workflow WorkflowSubmitter
}

func NewSettlementService(repo *repository.DepositRepository, db *sql.DB, finance *financeclient.Client, workflow WorkflowSubmitter) *SettlementService {
	return &SettlementService{repo: repo, db: db, finance: finance, workflow: workflow}
}

// OpenSavingsInput is the create-savings request body.
type OpenSavingsInput struct {
	SavingsCode    string `json:"savings_code"`
	CustomerCode   string `json:"customer_code"`
	ProductCode    string `json:"product_code"`
	OpenDate       string `json:"open_date"`
	PrincipalMinor int64  `json:"principal_minor"`
	CurrencyCode   string `json:"currency_code"`
	Actor          string `json:"-"`
	OrgCode        string `json:"-"`
}

// Open creates a savings account + OPEN transaction, posting the principal
// movement (DR cash / CR customer deposit liability).
func (s *SettlementService) Open(ctx context.Context, tenantID string, in *OpenSavingsInput) (*repository.Savings, error) {
	if strings.TrimSpace(in.SavingsCode) == "" || strings.TrimSpace(in.CustomerCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "savings_code and customer_code are required")
	}
	if in.PrincipalMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "principal_minor must be positive")
	}
	product, err := s.findProduct(ctx, tenantID, in.ProductCode)
	if err != nil {
		return nil, err
	}
	currency := in.CurrencyCode
	if currency == "" {
		currency = product.CurrencyCode
	}
	openDate := in.OpenDate
	if openDate == "" {
		openDate = todayDep(ctx)
	}
	maturity := addMonths(openDate, product.TermMonths)

	savings, err := s.repo.CreateSavings(ctx, &repository.Savings{
		TenantID:       tenantID,
		SavingsCode:    in.SavingsCode,
		CustomerCode:   in.CustomerCode,
		ProductCode:    product.ID,
		OpenDate:       openDate,
		MaturityDate:   maturity,
		PrincipalMinor: in.PrincipalMinor,
		CurrencyCode:   currency,
		OrgCode:        in.OrgCode,
		CreatedBy:      in.Actor,
	})
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	if _, err := s.repo.RecordTxn(ctx, &repository.DepositTxn{
		TenantID:     tenantID,
		SavingsID:    savings.ID,
		TxnType:      "OPEN",
		AmountMinor:  in.PrincipalMinor,
		CurrencyCode: currency,
		TxnDate:      openDate,
		CreatedBy:    in.Actor,
	}); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}

	// Posting: DR cash settlement, CR customer deposit liability.
	if s.finance != nil {
		entryID, err := s.post(ctx, tenantID, "DPM_OPEN", "DPM_OPEN", idempotencyKey("dpm-open", in.SavingsCode),
			savings.SavingsCode, savings.CustomerCode, openDate, currency, in.PrincipalMinor,
			"CASH_SETTLEMENT_ACCOUNT", "DPM_DEPOSIT_LIABILITY")
		if err != nil {
			return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "deposit posting failed", err)
		}
		if _, err := s.repo.RecordTxn(ctx, &repository.DepositTxn{
			TenantID:       tenantID,
			SavingsID:      savings.ID,
			TxnType:        "OPEN_POSTED",
			AmountMinor:    in.PrincipalMinor,
			CurrencyCode:   currency,
			TxnDate:        openDate,
			Status:         "POSTED",
			JournalEntryID: &entryID,
			CreatedBy:      in.Actor,
		}); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
	}
	return savings, nil
}

// SubmitSettle creates + submits the DPM_SETTLE_V2 maker/checker case. The
// actual settlement runs in the workflow execute step through the gRPC
// Settle callback (no more direct settle from the HTTP surface).
func (s *SettlementService) SubmitSettle(ctx context.Context, tenantID, actor, savingsCode string) (*Submission, error) {
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	savings, err := s.repo.GetSavingsByCode(ctx, tenantID, savingsCode)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "savings not found: "+savingsCode)
	}
	if savings.Status != "ACTIVE" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only ACTIVE savings can be settled")
	}
	key := idempotencyKey("dpm-settle", savingsCode)
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          "DPM_SETTLE_V2",
		Title:             "Tất toán sổ " + savingsCode,
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
	if _, err := s.workflow.SubmitCase(ctx, caseCreated.GetId(), actor,
		map[string]any{"savingsCode": savingsCode}, key+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	return &Submission{CaseID: caseCreated.GetId(), CaseCode: caseCreated.GetCaseCode()}, nil
}

// Settle closes a savings account: payout principal + accrued interest
// (DR customer deposit liability / CR cash). Runs in the workflow execute
// step through the gRPC callback.
func (s *SettlementService) Settle(ctx context.Context, tenantID, savingsCode, actor string) (*repository.Savings, error) {
	savings, err := s.repo.GetSavingsByCode(ctx, tenantID, savingsCode)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	if savings.Status != "ACTIVE" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only ACTIVE savings can be settled")
	}
	payoutMinor := savings.PrincipalMinor + savings.AccruedMinor
	var entryID string
	if s.finance != nil {
		var err error
		if savings.AccruedMinor > 0 {
			entryID, err = s.postSettlementV3(ctx, tenantID, savings)
		} else {
			entryID, err = s.post(ctx, tenantID, "DPM_SETTLEMENT", "DPM_SETTLEMENT", idempotencyKey("dpm-settlement", savingsCode),
				savings.SavingsCode, savings.CustomerCode, todayDep(ctx), savings.CurrencyCode, payoutMinor,
				"DPM_DEPOSIT_LIABILITY", "CASH_SETTLEMENT_ACCOUNT")
		}
		if err != nil {
			return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "settlement posting failed", err)
		}
	}
	if err := s.repo.CloseSavings(ctx, tenantID, savings.ID, entryID, actor); err != nil {
		return nil, mapErr(err)
	}
	return savings, nil
}

// postSettlementV3 posts the 3-leg settlement (principal + accrued interest +
// cash payout) when the savings has accrued interest (EPAS DPM.306 parity).
func (s *SettlementService) postSettlementV3(ctx context.Context, tenantID string, savings *repository.Savings) (string, error) {
	analytics := func() *financev1.Analytics {
		return &financev1.Analytics{
			CustomerCode: savings.CustomerCode,
			Dimensions:   map[string]string{"savings_code": savings.SavingsCode},
		}
	}
	payout := savings.PrincipalMinor + savings.AccruedMinor
	resp, err := s.finance.Post(ctx, &financev1.PostingRequest{
		IdempotencyKey: idempotencyKey("dpm-settlement", savings.SavingsCode),
		AccountingDate: todayDep(ctx),
		CurrencyCode:   savings.CurrencyCode,
		Description:    "DPM_SETTLEMENT_V3 " + savings.SavingsCode,
		BusinessReference: &financev1.BusinessReference{
			Domain:       "dpm",
			DocumentType: "DPM_SETTLEMENT_V3",
			DocumentCode: savings.SavingsCode,
		},
		Lines: financeclient.PostingLinesFromRules(
			financeclient.FetchPostingRules(s.finance, "DPM_SETTLEMENT_V3"),
			[]financeclient.PostingLeg{
				{CardLine: 1, Fallback: "DPM_DEPOSIT_LIABILITY", Direction: "DEBIT", AmountMinor: savings.PrincipalMinor, Analytics: analytics()},
				{CardLine: 2, Fallback: "DPM_INTEREST_PAYABLE", Direction: "DEBIT", AmountMinor: savings.AccruedMinor, Analytics: analytics()},
				{CardLine: 3, Fallback: "CASH_SETTLEMENT_ACCOUNT", Direction: "CREDIT", AmountMinor: payout, Analytics: analytics()},
			},
			savings.CurrencyCode,
		),
	})
	if err != nil {
		return "", err
	}
	return resp.GetJournalEntryId(), nil
}

// post builds and posts one two-leg movement. The rule card (cardType) is
// fetched first; fallback classifications keep unseeded environments working.
func (s *SettlementService) post(ctx context.Context, tenantID, refType, cardType, idemKey, savingsCode, customerCode, accountingDate, currency string, amountMinor int64, debitFallback, creditFallback string) (string, error) {
	analytics := func() *financev1.Analytics {
		return &financev1.Analytics{
			CustomerCode: customerCode,
			Dimensions:   map[string]string{"savings_code": savingsCode},
		}
	}
	postReq := &financev1.PostingRequest{
		IdempotencyKey: idemKey,
		AccountingDate: accountingDate,
		CurrencyCode:   currency,
		Description:    refType + " " + savingsCode,
		BusinessReference: &financev1.BusinessReference{
			Domain:       "dpm",
			DocumentType: refType,
			DocumentCode: savingsCode,
		},
		Lines: financeclient.PostingLinesFromRules(
			financeclient.FetchPostingRules(s.finance, cardType),
			[]financeclient.PostingLeg{
				{CardLine: 1, Fallback: debitFallback, Direction: "DEBIT", AmountMinor: amountMinor, Analytics: analytics()},
				{CardLine: 2, Fallback: creditFallback, Direction: "CREDIT", AmountMinor: amountMinor, Analytics: analytics()},
			},
			currency,
		),
	}
	resp, err := s.finance.Post(ctx, postReq)
	if err != nil {
		return "", err
	}
	return resp.GetJournalEntryId(), nil
}

// idempotencyKey returns "<prefix>-<code>-<random>" so distinct submissions
// never replay each other; the random suffix is the uniqueness component.
func idempotencyKey(prefix, code string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%s", prefix, code)
	}
	return prefix + "-" + code + "-" + hex.EncodeToString(buf)
}

func (s *SettlementService) findProduct(ctx context.Context, tenantID, code string) (*repository.SavingsProduct, error) {
	active := true
	products, err := s.repo.ListProducts(ctx, repository.ListProductsParams{TenantID: tenantID, IsActive: &active})
	if err != nil {
		return nil, mapErr(err)
	}
	for _, p := range products {
		if p.Code == code {
			return &p, nil
		}
	}
	return nil, ardaerrors.New(ardaerrors.CodeNotFound, "product not found: "+code)
}

func mapErr(err error) error {
	return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
}

func todayDep(ctx context.Context) string {
	return ardatime.TodayCtx(ctx)
}

func addMonths(dateStr string, months int) string {
	// Clamp month-end overflow (Jan 31 + 1 month = Feb 28/29, never Mar 2/3).
	m, err := ardatime.AddMonthsClamped(dateStr, months)
	if err != nil {
		return dateStr
	}
	return m
}

// ListSavings passthrough for the HTTP read API.
func (s *SettlementService) ListSavings(ctx context.Context, tenantID string, orgCodes []string, status, q string) ([]repository.Savings, error) {
	return s.repo.ListSavings(ctx, tenantID, orgCodes, status, q)
}

// ListProducts passthrough.
func (s *SettlementService) ListProducts(ctx context.Context, params repository.ListProductsParams) ([]repository.SavingsProduct, error) {
	return s.repo.ListProducts(ctx, params)
}

// UpsertProduct creates or updates one deposit product.
func (s *SettlementService) UpsertProduct(ctx context.Context, tenantID, actor string, in *repository.SavingsProduct) (*repository.SavingsProduct, error) {
	if in.Code == "" || in.Name == "" || in.TermMonths <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code, name and a positive term_months are required")
	}
	in.ID = ""
	in.TenantID = tenantID
	in.CreatedBy = actor
	return s.repo.UpsertProduct(ctx, in)
}

// ListInterbank passthrough.
func (s *SettlementService) ListInterbank(ctx context.Context, tenantID string, orgCodes []string, status string) ([]repository.InterbankDeposit, error) {
	return s.repo.ListInterbankDeposits(ctx, tenantID, orgCodes, status)
}

// GetSavingsByCode passthrough for the gRPC callback surface.
func (s *SettlementService) GetSavingsByCode(ctx context.Context, tenantID, code string) (*repository.Savings, error) {
	return s.repo.GetSavingsByCode(ctx, tenantID, code)
}
