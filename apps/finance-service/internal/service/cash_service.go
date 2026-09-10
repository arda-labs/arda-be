package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// CashService records treasury cash transactions (VCM kho quỹ) and posts
// them through the PostingService in-process (finance owns the VCM domain
// per Q3 — no gRPC hop).
type CashService struct {
	db      *sql.DB
	posting *PostingService
}

func NewCashService(db *sql.DB, posting *PostingService) *CashService {
	return &CashService{db: db, posting: posting}
}

// CashTxnInput is one cash in/out movement.
type CashTxnInput struct {
	TxnDate      string `json:"txn_date"`
	Direction    string `json:"direction"`
	AmountMinor  int64  `json:"amount_minor"`
	CurrencyCode string `json:"currency_code"`
	OrgCode      string `json:"org_code"`
	Description  string `json:"description"`
	Actor        string `json:"-"`
}

// Record posts a cash movement: IN = DR cash / CR counterparty cash account;
// OUT = the mirror. Uses the LNM.300.01-adjacent VCM rule (cash settlement).
func (s *CashService) Record(ctx context.Context, tenantID string, in *CashTxnInput) (*CashTxnInput, error) {
	if in.Direction != "IN" && in.Direction != "OUT" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "direction must be IN or OUT")
	}
	if in.AmountMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_minor must be positive")
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	if in.TxnDate == "" {
		in.TxnDate = ardatime.Today()
	}
	if in.TxnDate == "" || len(in.TxnDate) != 10 || in.TxnDate[4] != '-' || in.TxnDate[7] != '-' {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "txn_date must be YYYY-MM-DD")
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO fin_cash_transactions (tenant_id, txn_date, direction, amount_minor, currency_code, org_code, description, created_by)
		VALUES ($1,$2::date,$3,$4,$5,$6,$7,$8)`,
		tenantID, in.TxnDate, in.Direction, in.AmountMinor, in.CurrencyCode, in.OrgCode, in.Description, in.Actor); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}

	if s.posting != nil {
		_ = ardamoney.FromMinor(in.AmountMinor, in.CurrencyCode)
		docType := "VCM_CASH_IN"
		debit, credit := "CASH_SETTLEMENT_ACCOUNT", "FUND_DISBURSEMENT_IN_TRANSIT"
		if in.Direction == "OUT" {
			docType = "VCM_CASH_OUT"
			debit, credit = "FUND_DISBURSEMENT_IN_TRANSIT", "CASH_SETTLEMENT_ACCOUNT"
		}
		req := &financev1.PostingRequest{
			IdempotencyKey: fmt.Sprintf("vcm-%s-%s-%s", strings.ToLower(in.Direction), in.TxnDate, fmt.Sprint(in.AmountMinor)),
			AccountingDate: in.TxnDate,
			CurrencyCode:   in.CurrencyCode,
			Description:    "Kho quỹ " + in.Direction,
			BusinessReference: &financev1.BusinessReference{
				Domain:       "vcm",
				DocumentType: docType,
				DocumentCode: in.Description,
			},
			Lines: []*financev1.PostingLine{
				{
					LineNo:       1,
					Direction:    "DEBIT",
					AmountMinor:  in.AmountMinor,
					CurrencyCode: in.CurrencyCode,
					Analytics: &financev1.Analytics{
						AccClassification: debit,
						OrgUnitCode:       in.OrgCode,
					},
				},
				{
					LineNo:       2,
					Direction:    "CREDIT",
					AmountMinor:  in.AmountMinor,
					CurrencyCode: in.CurrencyCode,
					Analytics: &financev1.Analytics{
						AccClassification: credit,
						OrgUnitCode:       in.OrgCode,
					},
				},
			},
		}
		if _, err := s.posting.PostTransaction(ctx, tenantID, req); err != nil {
			return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "cash posting failed", err)
		}
	}
	return in, nil
}

// CashPosition aggregates in/out per day for the treasury aggregate screen.
func (s *CashService) Position(ctx context.Context, tenantID string) ([]CashPositionRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT txn_date::text, currency_code,
		       COALESCE(SUM(CASE WHEN direction='IN' THEN amount_minor ELSE 0 END),0) AS cash_in,
		       COALESCE(SUM(CASE WHEN direction='OUT' THEN amount_minor ELSE 0 END),0) AS cash_out
		FROM fin_cash_transactions WHERE tenant_id = $1
		GROUP BY 1,2 ORDER BY 1 DESC LIMIT 90`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CashPositionRow{}
	for rows.Next() {
		var r CashPositionRow
		if err := rows.Scan(&r.TxnDate, &r.CurrencyCode, &r.CashInMinor, &r.CashOutMinor); err != nil {
			return nil, err
		}
		r.NetMinor = r.CashInMinor - r.CashOutMinor
		out = append(out, r)
	}
	return out, rows.Err()
}

// CashPositionRow is one aggregate row.
type CashPositionRow struct {
	TxnDate      string `json:"txn_date"`
	CurrencyCode string `json:"currency_code"`
	CashInMinor  int64  `json:"cash_in_minor"`
	CashOutMinor int64  `json:"cash_out_minor"`
	NetMinor     int64  `json:"net_minor"`
}
