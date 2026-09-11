package repository

import (
	"context"
	"fmt"
	"strings"
)

// LoanLedgerRow is one sổ/sao kê khoản vay movement row (W4c).
type LoanLedgerRow struct {
	TxnDate       string `json:"txn_date"`
	ContractCode  string `json:"contract_code"`
	TxnType       string `json:"txn_type"`
	AmountMinor   int64  `json:"amount_minor"`
	PrincipalMinor int64 `json:"principal_minor"`
	InterestMinor int64  `json:"interest_minor"`
	CurrencyCode  string `json:"currency_code"`
	Status        string `json:"status"`
}

// LoanStatementRow is one khoản vay statement row (agreement snapshot).
type LoanStatementRow struct {
	ContractCode         string  `json:"contract_code"`
	AgreementCode        string  `json:"agreement_code"`
	DisburseDate         string  `json:"disburse_date,omitempty"`
	MaturityDate         string  `json:"maturity_date,omitempty"`
	DebtGroupCode        string  `json:"debt_group_code"`
	DisburseAmtMinor     int64   `json:"disburse_amt_minor"`
	OutstandingAmtMinor  int64   `json:"outstanding_amt_minor"`
	ColnPrincipalMinor   int64   `json:"coln_principal_amt_minor"`
	ColnInterestMinor    int64   `json:"coln_interest_amt_minor"`
	InterestRate         float64 `json:"interest_rate"`
	Status               string  `json:"status"`
}

// CollateralStatementRow is one sổ tài sản bảo đảm row.
type CollateralStatementRow struct {
	CollCode         string `json:"coll_code"`
	CollName         string `json:"coll_name"`
	CollTypeCode     string `json:"coll_type_code,omitempty"`
	MortgageCode     string `json:"mortgage_code,omitempty"`
	OwnerCifCode     string `json:"owner_cif_code,omitempty"`
	OwnerName        string `json:"owner_name,omitempty"`
	CollValueMinor   int64  `json:"coll_value_minor"`
	CollUseValueMinor int64 `json:"coll_use_value_minor"`
	ValuationDate    string `json:"valuation_date,omitempty"`
	Status           string `json:"status"`
}

// LoanLedger unions POSTED disbursements and collections in [from, to].
func (r *LoanRepository) LoanLedger(ctx context.Context, tenantID, fromDate, toDate, contractCode string) ([]LoanLedgerRow, error) {
	disbWhere := []string{"tenant_id = $1", "status = 'POSTED'"}
	disbArgs := []any{tenantID}
	if fromDate != "" {
		disbArgs = append(disbArgs, fromDate)
		disbWhere = append(disbWhere, fmt.Sprintf("disburse_date >= $%d::date", len(disbArgs)))
	}
	if toDate != "" {
		disbArgs = append(disbArgs, toDate)
		disbWhere = append(disbWhere, fmt.Sprintf("disburse_date <= $%d::date", len(disbArgs)))
	}
	if contractCode != "" {
		disbArgs = append(disbArgs, contractCode)
		disbWhere = append(disbWhere, fmt.Sprintf("contract_code = $%d::text", len(disbArgs)))
	}

	colWhere := []string{"tenant_id = $1", "status = 'POSTED'"}
	colArgs := []any{tenantID}
	if fromDate != "" {
		colArgs = append(colArgs, fromDate)
		colWhere = append(colWhere, fmt.Sprintf("collection_date >= $%d::date", len(colArgs)))
	}
	if toDate != "" {
		colArgs = append(colArgs, toDate)
		colWhere = append(colWhere, fmt.Sprintf("collection_date <= $%d::date", len(colArgs)))
	}
	if contractCode != "" {
		colArgs = append(colArgs, contractCode)
		colWhere = append(colWhere, fmt.Sprintf("contract_code = $%d::text", len(colArgs)))
	}

	query := fmt.Sprintf(`
		SELECT disburse_date::text, contract_code, 'DISBURSEMENT', disburse_amt_minor, 0, 0, currency_code, status
		FROM lnm_disbursements WHERE %s
		UNION ALL
		SELECT collection_date::text, contract_code, 'COLLECTION', 0, principal_minor, interest_minor, currency_code, status
		FROM lnm_collections WHERE %s
		ORDER BY 1 DESC, 2 LIMIT 2000`,
		strings.Join(disbWhere, " AND "), strings.Join(colWhere, " AND "))
	args := append(append([]any{}, disbArgs...), colArgs...)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoanLedgerRow{}
	for rows.Next() {
		var x LoanLedgerRow
		if err := rows.Scan(&x.TxnDate, &x.ContractCode, &x.TxnType, &x.AmountMinor,
			&x.PrincipalMinor, &x.InterestMinor, &x.CurrencyCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// LoanStatement lists agreements (optionally one contract).
func (r *LoanRepository) LoanStatement(ctx context.Context, tenantID, contractCode string) ([]LoanStatementRow, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if contractCode != "" {
		args = append(args, contractCode)
		where = append(where, fmt.Sprintf("contract_code = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT contract_code, agreement_code, COALESCE(disburse_date::text,''), COALESCE(maturity_date::text,''),
		       debt_group_code, disburse_amt_minor, outstanding_amt_minor, coln_principal_amt_minor,
		       coln_interest_amt_minor, COALESCE(interest_rate, 0), status
		FROM lnm_agreements WHERE `+strings.Join(where, " AND ")+`
		ORDER BY contract_code, agreement_code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoanStatementRow{}
	for rows.Next() {
		var x LoanStatementRow
		if err := rows.Scan(&x.ContractCode, &x.AgreementCode, &x.DisburseDate, &x.MaturityDate,
			&x.DebtGroupCode, &x.DisburseAmtMinor, &x.OutstandingAmtMinor, &x.ColnPrincipalMinor,
			&x.ColnInterestMinor, &x.InterestRate, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// CollateralStatement lists collateral rows by status.
func (r *LoanRepository) CollateralStatement(ctx context.Context, tenantID, status string) ([]CollateralStatementRow, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT coll_code, coll_name, COALESCE(coll_type_code,''), COALESCE(mortgage_code,''),
		       COALESCE(owner_cif_code,''), COALESCE(owner_name,''), coll_value_minor,
		       coll_use_value_minor, COALESCE(valuation_date::text,''), status
		FROM lnm_collaterals WHERE `+strings.Join(where, " AND ")+`
		ORDER BY coll_code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CollateralStatementRow{}
	for rows.Next() {
		var x CollateralStatementRow
		if err := rows.Scan(&x.CollCode, &x.CollName, &x.CollTypeCode, &x.MortgageCode, &x.OwnerCifCode,
			&x.OwnerName, &x.CollValueMinor, &x.CollUseValueMinor, &x.ValuationDate, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
