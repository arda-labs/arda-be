package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/shopspring/decimal"
)

// SettlementService runs deposit open/settle movements. Posting goes through
// the finance PostingService (rule card DPM_SETTLEMENT — see
// docs/accounting-rule-cards.md; add DPM-specific cards when the flows land).
type SettlementService struct {
	repo    *repository.DepositRepository
	db      *sql.DB
	finance *financeclient.Client
}

func NewSettlementService(repo *repository.DepositRepository, db *sql.DB, finance *financeclient.Client) *SettlementService {
	return &SettlementService{repo: repo, db: db, finance: finance}
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
		openDate = todayDep()
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
		entryID, err := s.post(ctx, tenantID, "DPM_OPEN", savings.SavingsCode, savings.CustomerCode,
			openDate, currency, in.PrincipalMinor, "DPM_DEPOSIT_LIABILITY", "CASH_SETTLEMENT_ACCOUNT")
		if err != nil {
			return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "deposit posting failed", err)
		}
		if _, err := s.repo.RecordTxn(ctx, &repository.DepositTxn{
			TenantID:     tenantID,
			SavingsID:    savings.ID,
			TxnType:      "OPEN_POSTED",
			AmountMinor:  in.PrincipalMinor,
			CurrencyCode: currency,
			TxnDate:      openDate,
			Status:       "POSTED",
			CreatedBy:    in.Actor,
		}); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		_ = entryID
	}
	return savings, nil
}

// Settle closes a savings account: payout principal + accrued interest
// (DR customer deposit liability + DR interest expense / CR cash).
func (s *SettlementService) Settle(ctx context.Context, tenantID, savingsCode, actor string) (*repository.Savings, error) {
	savings, err := s.repo.GetSavingsByCode(ctx, tenantID, savingsCode)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	if savings.Status != "ACTIVE" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only ACTIVE savings can be settled")
	}
	payoutMinor := savings.PrincipalMinor + savings.AccruedMinor
	if s.finance != nil {
		if _, err := s.post(ctx, tenantID, "DPM_SETTLE", savings.SavingsCode, savings.CustomerCode,
			todayDep(), savings.CurrencyCode, payoutMinor,
			"DPM_DEPOSIT_LIABILITY", "CASH_SETTLEMENT_ACCOUNT"); err != nil {
			return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "settlement posting failed", err)
		}
	}
	if err := s.repo.CloseSavings(ctx, tenantID, savings.ID, "", actor); err != nil {
		return nil, mapErr(err)
	}
	return savings, nil
}

func (s *SettlementService) post(ctx context.Context, tenantID, docType, savingsCode, customerCode, accountingDate, currency string, amountMinor int64, creditClass, debitClass string) (string, error) {
	amount := ardamoney.FromMinor(amountMinor, currency)
	_ = amount
	_ = decimal.Decimal{}
	postReq := &financev1.PostingRequest{
		IdempotencyKey: fmt.Sprintf("dpm-%s-%s", strings.ToLower(docType), savingsCode),
		AccountingDate: accountingDate,
		CurrencyCode:   currency,
		Description:    docType + " " + savingsCode,
		BusinessReference: &financev1.BusinessReference{
			Domain:       "dpm",
			DocumentType: docType,
			DocumentCode: savingsCode,
		},
		Lines: []*financev1.PostingLine{
			{
				LineNo:       1,
				Direction:    "DEBIT",
				AmountMinor:  amountMinor,
				CurrencyCode: currency,
				Analytics: &financev1.Analytics{
					AccClassification: debitClass,
					CustomerCode:      customerCode,
				},
			},
			{
				LineNo:       2,
				Direction:    "CREDIT",
				AmountMinor:  amountMinor,
				CurrencyCode: currency,
				Analytics: &financev1.Analytics{
					AccClassification: creditClass,
					CustomerCode:      customerCode,
					Dimensions:        map[string]string{"savings_code": savingsCode},
				},
			},
		},
	}
	resp, err := s.finance.Post(ctx, postReq)
	if err != nil {
		return "", err
	}
	return resp.GetJournalEntryId(), nil
}

func (s *SettlementService) findProduct(ctx context.Context, tenantID, code string) (*repository.SavingsProduct, error) {
	products, err := s.repo.ListProducts(ctx, tenantID)
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

func todayDep() string {
	return time.Now().Format("2006-01-02")
}

func addMonths(dateStr string, months int) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return t.AddDate(0, months, 0).Format("2006-01-02")
}

// ListSavings passthrough for the HTTP read API.
func (s *SettlementService) ListSavings(ctx context.Context, tenantID string, orgCodes []string, status, q string) ([]repository.Savings, error) {
	return s.repo.ListSavings(ctx, tenantID, orgCodes, status, q)
}

// ListProducts passthrough.
func (s *SettlementService) ListProducts(ctx context.Context, tenantID string) ([]repository.SavingsProduct, error) {
	return s.repo.ListProducts(ctx, tenantID)
}

// ListInterbank passthrough.
func (s *SettlementService) ListInterbank(ctx context.Context, tenantID string, orgCodes []string, status string) ([]repository.InterbankDeposit, error) {
	return s.repo.ListInterbankDeposits(ctx, tenantID, orgCodes, status)
}
