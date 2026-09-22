package repository

import (
	"context"
)

// ReportingCapitalContract is the reporting read-model projection of one fund
// contract. It carries org_code (the dimension key) and the measurement
// columns; internal row id, product code, workflow/journal linkage and audit
// fields are dropped.
type ReportingCapitalContract struct {
	ContractCode     string  `json:"contract_code"`
	FundTypeCode     string  `json:"fund_type_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	OrgCode          string  `json:"org_code"`
	ContractDate     string  `json:"contract_date"`
	MaturityDate     string  `json:"maturity_date"`
	AmountMinor      int64   `json:"amount_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	Status           string  `json:"status"`
}

// ReportingCapitalMovement is the reporting read-model projection of one fund
// movement, joined to its contract code.
type ReportingCapitalMovement struct {
	MovementID   string `json:"movement_id"`
	ContractCode string `json:"contract_code"`
	MovementType string `json:"movement_type"`
	MovementDate string `json:"movement_date"`
	AmountMinor  int64  `json:"amount_minor"`
	CurrencyCode string `json:"currency_code"`
	Status       string `json:"status"`
}

// ListContractsForReporting returns every fund contract of the tenant
// (optionally one org) in the reporting projection, un-paged for the ETL.
func (r *CapitalRepository) ListContractsForReporting(ctx context.Context, tenantID, orgCode string) ([]ReportingCapitalContract, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT contract_code, fund_type_code, counterparty_code, COALESCE(org_code,''),
		       contract_date::text, COALESCE(maturity_date::text,''), amount_minor,
		       interest_rate, currency_code, status
		FROM cfc_contracts
		WHERE tenant_id = $1 AND ($2 = '' OR org_code = $2)
		ORDER BY contract_code`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportingCapitalContract{}
	for rows.Next() {
		var x ReportingCapitalContract
		if err := rows.Scan(&x.ContractCode, &x.FundTypeCode, &x.CounterpartyCode, &x.OrgCode,
			&x.ContractDate, &x.MaturityDate, &x.AmountMinor, &x.InterestRate,
			&x.CurrencyCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ListMovementsForReporting returns every fund movement of the tenant joined to
// its contract code (movements link by contract_id UUID).
func (r *CapitalRepository) ListMovementsForReporting(ctx context.Context, tenantID string) ([]ReportingCapitalMovement, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id::text, c.contract_code, m.movement_type, m.movement_date::text,
		       m.amount_minor, m.currency_code, m.status
		FROM cfc_movements m JOIN cfc_contracts c ON c.id = m.contract_id
		WHERE m.tenant_id = $1
		ORDER BY m.movement_date, c.contract_code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportingCapitalMovement{}
	for rows.Next() {
		var x ReportingCapitalMovement
		if err := rows.Scan(&x.MovementID, &x.ContractCode, &x.MovementType, &x.MovementDate,
			&x.AmountMinor, &x.CurrencyCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
