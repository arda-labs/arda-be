package domain

import (
	"encoding/json"
	"time"
)

// Contract statuses.
const (
	ContractDraft   = "DRAFT"
	ContractPending = "PENDING"
	ContractActive  = "ACTIVE"
	ContractRejected = "REJECTED"
	ContractClosed  = "CLOSED"
)

// Adjustment statuses.
const (
	AdjustmentDraft    = "DRAFT"
	AdjustmentPending  = "PENDING"
	AdjustmentActive   = "ACTIVE"
	AdjustmentRejected = "REJECTED"
	AdjustmentCancelled = "CANCELLED"
)

// Contract is the credit contract header (EPAS lnm_inf_contract subset — the
// snapshot tables _a/_h are deliberate P1 omissions; balances live on
// agreements and are computed on demand).
type Contract struct {
	ID                    string          `json:"id"`
	TenantID              string          `json:"tenant_id"`
	ContractCode          string          `json:"contract_code"`
	ContractNo            string          `json:"contract_no"`
	CustomerCode          string          `json:"customer_code"`
	EmployeeCode          string          `json:"employee_code"`
	ContractTypeCode      string          `json:"contract_type_code"`
	ProductCode           string          `json:"product_code"`
	InterestRate          float64         `json:"interest_rate"`
	InterestRateType      string          `json:"interest_rate_type"`
	PurposeCode           string          `json:"purpose_code"`
	IndustryCode          string          `json:"industry_code"`
	LoanMethodCode        string          `json:"loan_method_code"`
	ContractDate          string          `json:"contract_date"`
	LoanTerm              int             `json:"loan_term"`
	TermUnit              string          `json:"term_unit"`
	MaturityDate          string          `json:"maturity_date"`
	InterestScheduleDay   int             `json:"interest_schedule_day"`
	LoanAmt               int64           `json:"loan_amt_minor"`
	InterestPaymentFreq   string          `json:"interest_payment_freq"`
	PrincipalPaymentFreq  string          `json:"principal_payment_freq"`
	InterestPaymentMethod string          `json:"interest_payment_method"`
	PrincipalPaymentMethod string         `json:"principal_payment_method"`
	Status                string          `json:"status"`
	WorkflowCaseID        *string         `json:"workflow_case_id,omitempty"`
	CreatedBy             string          `json:"created_by"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

// Agreement is one drawdown (disbursement) against a contract, carrying the
// running balances EPAS kept on lnm_inf_agreement (core subset).
type Agreement struct {
	ID                   string    `json:"id"`
	TenantID             string    `json:"tenant_id"`
	ContractCode         string    `json:"contract_code"`
	AgreementCode        string    `json:"agreement_code"`
	DisburseDate         string    `json:"disburse_date"`
	DisburseAmt          int64     `json:"disburse_amt_minor"`
	InterestRate         float64   `json:"interest_rate"`
	OverInterestRate     float64   `json:"over_interest_rate"`
	LoanTerm             int       `json:"loan_term"`
	TermUnit             string    `json:"term_unit"`
	MaturityDate         string    `json:"maturity_date"`
	DebtGroupCode        string    `json:"debt_group_code"`
	InterestPaymentFreq  string    `json:"interest_payment_freq"`
	PrincipalPaymentFreq string    `json:"principal_payment_freq"`
	OutstandingAmt       int64     `json:"outstanding_amt_minor"`
	ColnPrincipalAmt     int64     `json:"coln_principal_amt_minor"`
	ColnInterestAmt      int64     `json:"coln_interest_amt_minor"`
	ProvisionAmt         int64     `json:"provision_amt_minor"`
	CurrencyCode         string    `json:"currency_code"`
	AccClassification    string    `json:"acc_classification"`
	Status               string    `json:"status"`
	CreatedBy            string    `json:"created_by"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// RepayPlan is one schedule row (EPAS lnm_inf_repay_plan).
type RepayPlan struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	ContractCode     string    `json:"contract_code"`
	AgreementCode    string    `json:"agreement_code"`
	PlanNo           int       `json:"plan_no"`
	TermNo           int       `json:"term_no"`
	FromDate         string    `json:"from_date"`
	ToDate           string    `json:"to_date"`
	InterestRate     float64   `json:"interest_rate"`
	PlanPrincipalAmt int64     `json:"plan_principal_amt_minor"`
	PlanInterestAmt  int64     `json:"plan_interest_amt_minor"`
	ColnPrincipalAmt int64     `json:"coln_principal_amt_minor"`
	ColnInterestAmt  int64     `json:"coln_interest_amt_minor"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Mortgage is the collateral registration envelope (EPAS lnm_inf_mortgage).
type Mortgage struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	MortgageCode     string    `json:"mortgage_code"`
	MortgageNo       string    `json:"mortgage_no"`
	CustomerCode     string    `json:"customer_code"`
	MortgageDate     string    `json:"mortgage_date"`
	NotarizationDate string    `json:"notarization_date"`
	RegistrationDate string    `json:"registration_date"`
	ExpireDate       string    `json:"expire_date"`
	Status           string    `json:"status"`
	Description      *string   `json:"description,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Collateral is one collateral asset (EPAS lnm_inf_coll).
type Collateral struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	CollCode       string    `json:"coll_code"`
	CollName       string    `json:"coll_name"`
	CollTypeCode   string    `json:"coll_type_code"`
	MortgageCode   string    `json:"mortgage_code"`
	OwnerCifCode   string    `json:"owner_cif_code"`
	OwnerName      string    `json:"owner_name"`
	CollAddress    string    `json:"coll_address"`
	Quantity       float64   `json:"quantity"`
	UnitPrice      int64     `json:"unit_price_minor"`
	CollValue      int64     `json:"coll_value_minor"`
	CollUseValue   int64     `json:"coll_use_value_minor"`
	ValuationDate  string    `json:"valuation_date"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ContractCollateral links a contract to a collateral asset with an
// allocated value (EPAS lnm_inf_contract_coll).
type ContractCollateral struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	ContractCode string   `json:"contract_code"`
	CollCode    string    `json:"coll_code"`
	CollValue   int64     `json:"coll_value_minor"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Adjustment is the uniform shape of every loan adjustment flow (debt group
// change, rate change, restructure, waiver, writeoff, recovery, fund check,
// revenue allocation, VFU fee allocation, off-balance export). Flow-specific
// fields live in `payload` (jsonb) validated per kind by the service layer —
// same uniform-catalog pattern as mdm-service.
type Adjustment struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	Kind           string          `json:"kind"`
	ContractCode   string          `json:"contract_code"`
	AgreementCode  *string         `json:"agreement_code,omitempty"`
	EffectiveDate  *string         `json:"effective_date,omitempty"`
	Amount         *int64          `json:"amount_minor,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Status         string          `json:"status"`
	WorkflowCaseID *string         `json:"workflow_case_id,omitempty"`
	DecisionNote   *string         `json:"decision_note,omitempty"`
	DecidedBy      *string         `json:"decided_by,omitempty"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Agreement gains the product's accounting classification for COA resolution.
// (See lnm_agreements.acc_classification.)

// VfuParty is a trust/mandate counterparty (EPAS lnm_inf_vfu_party).
type VfuParty struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	PartyCode          string    `json:"party_code"`
	PartyName          string    `json:"party_name"`
	PartyType          string    `json:"party_type"`
	GenderCode         string    `json:"gender_code"`
	DateOfBirth        string    `json:"date_of_birth"`
	IdentificationID   string    `json:"identification_id"`
	IssueDate          string    `json:"issue_date"`
	IssuePlace         string    `json:"issue_place"`
	MobileNumber       string    `json:"mobile_number"`
	PermanentAddress   string    `json:"permanent_address"`
	CustomerRelnCode   string    `json:"customer_reln_code"`
	Status             string    `json:"status"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// VfuMandate is a trust mandate contract (EPAS lnm_inf_vfu_contract_mandate).
type VfuMandate struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	MandateCode    string    `json:"mandate_code"`
	MandateNo      string    `json:"mandate_no"`
	MandateDate    string    `json:"mandate_date"`
	PartyCode      string    `json:"party_code"`
	OrgCode        string    `json:"org_code"`
	RepName        string    `json:"rep_name"`
	RepPhone       string    `json:"rep_phone"`
	RepAddress     string    `json:"rep_address"`
	BankName       string    `json:"bank_name"`
	BankAccount    string    `json:"bank_account"`
	FeePaymentFreq string    `json:"fee_payment_freq"`
	RateValue      *float64  `json:"rate_value,omitempty"`
	Status         string    `json:"status"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// VfuPlan is a funding plan row under a mandate (EPAS lnm_inf_vfu_plan).
type VfuPlan struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	PlanCode     string    `json:"plan_code"`
	PlanDate     string    `json:"plan_date"`
	MandateCode  string    `json:"mandate_code"`
	ContractCode string    `json:"contract_code"`
	AllocatedAmt int64     `json:"allocated_amt_minor"`
	SettledAmt   int64     `json:"settled_amt_minor"`
	FeeAmt       int64     `json:"fee_amt_minor"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Accrual is one computed interest accrual row (EOD batch, P1b.4b).
type Accrual struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	AgreementCode  string    `json:"agreement_code"`
	FromDate       string    `json:"from_date"`
	ToDate         string    `json:"to_date"`
	InterestMinor  int64     `json:"interest_minor"`
	CurrencyCode   string    `json:"currency_code"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}
