package domain

// ReportingAgreement is the reporting read-model projection of one drawdown.
// Unlike the chat-facing Agreement it carries org_code (the RLS/dimension key)
// and the measurement columns the statistical ETL needs, and nothing else —
// it is an ETL source, not a general domain view.
type ReportingAgreement struct {
	AgreementCode       string  `json:"agreement_code"`
	ContractCode        string  `json:"contract_code"`
	CustomerCode        string  `json:"customer_code"`
	ProductCode         string  `json:"product_code"`
	OrgCode             string  `json:"org_code"`
	DisburseDate        string  `json:"disburse_date"`
	MaturityDate        string  `json:"maturity_date"`
	DebtGroupCode       string  `json:"debt_group_code"`
	Status              string  `json:"status"`
	CurrencyCode        string  `json:"currency_code"`
	InterestRate        float64 `json:"interest_rate"`
	DisburseAmtMinor    int64   `json:"disburse_amt_minor"`
	OutstandingAmtMinor int64   `json:"outstanding_amt_minor"`
	ProvisionAmtMinor   int64   `json:"provision_amt_minor"`

	// Contract-level attributes the PCF credit indicators slice on: the loan
	// method (từng lần / hạn mức / cầm cố), the industry (nông nghiệp / phi
	// nông nghiệp) and the term normalised to months plus its bucket
	// (ngắn hạn ≤ 12 tháng vs trung dài hạn).
	LoanMethodCode string `json:"loan_method_code"`
	IndustryCode   string `json:"industry_code"`
	PurposeCode    string `json:"purpose_code"`
	LoanTermMonths int    `json:"loan_term_months"`
	TermBucket     string `json:"term_bucket"`
}

// ReportingCollateral is the reporting read-model projection of one collateral.
type ReportingCollateral struct {
	CollCode          string `json:"coll_code"`
	CollTypeCode      string `json:"coll_type_code"`
	OrgCode           string `json:"org_code"`
	ValuationDate     string `json:"valuation_date"`
	Status            string `json:"status"`
	CollValueMinor    int64  `json:"coll_value_minor"`
	CollUseValueMinor int64  `json:"coll_use_value_minor"`
}
