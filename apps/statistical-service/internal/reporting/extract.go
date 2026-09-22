// Package reporting holds the reporting ETL: statistical-service pulls the
// tenant slice from each domain service over the signed /internal/reporting/*
// surface and materialises the fact read model. It never reads a domain
// database directly (arda-be/docs/reporting-data-layer.md).
package reporting

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const (
	extractTimeout     = 30 * time.Second
	extractMaxResponse = 8 << 20 // 8 MB — one tenant slice per source
	signTTL            = 5 * time.Minute
)

// Service materialises the reporting fact tables from the domain services.
type Service struct {
	db         *sql.DB
	secret     string
	loanURL    string
	depositURL string
	capitalURL string
	crmURL     string
	financeURL string
	client     *http.Client
	logger     *slog.Logger
}

func NewService(db *sql.DB, secret, loanURL, depositURL, capitalURL, crmURL, financeURL string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	trim := func(s string) string { return strings.TrimRight(strings.TrimSpace(s), "/") }
	return &Service{
		db:         db,
		secret:     secret,
		loanURL:    trim(loanURL),
		depositURL: trim(depositURL),
		capitalURL: trim(capitalURL),
		crmURL:     trim(crmURL),
		financeURL: trim(financeURL),
		client:     &http.Client{Timeout: extractTimeout},
		logger:     logger,
	}
}

// ExtractResult summarizes one ETL run.
type ExtractResult struct {
	BusinessDate     string   `json:"business_date"`
	LoanAgreements   int      `json:"loan_agreements"`
	LoanCollaterals  int      `json:"loan_collaterals"`
	DepositSavings   int      `json:"deposit_savings"`
	CapitalContracts int      `json:"capital_contracts"`
	CapitalMovements int      `json:"capital_movements"`
	Customers        int      `json:"customers"`
	Members          int      `json:"members"`
	MemberRequests   int      `json:"member_requests"`
	IBMDeposits      int      `json:"ibm_deposits"`
	IBMBorrows       int      `json:"ibm_borrows"`
	TrialBalance     int      `json:"trial_balance_rows"`
	SkippedSources   []string `json:"skipped_sources,omitempty"`
}

// ExtractDaily rebuilds the fact rows for (tenant, business_date). It is
// idempotent: each source's slice is replaced inside one transaction, so a
// re-run or backfill of a COB date is always safe. A configured-but-failing
// source fails the run (fail loud); an unconfigured source is skipped and
// reported so local environments can wire one service at a time.
func (s *Service) ExtractDaily(ctx context.Context, tenantID, businessDate string) (ExtractResult, error) {
	result := ExtractResult{BusinessDate: businessDate}
	if strings.TrimSpace(tenantID) == "" {
		return result, fmt.Errorf("tenant scope is required")
	}
	if _, err := time.Parse("2006-01-02", businessDate); err != nil {
		return result, fmt.Errorf("business_date must be YYYY-MM-DD")
	}

	var (
		agreements  []loanAgreement
		collaterals []loanCollateral
		savings     []depositSavings
		contracts   []capitalContract
		movements   []capitalMovement
		customers   []customer
		members     []member
		memberReqs  []memberRequest
		ibmDeposits []ibmDeposit
		ibmBorrows  []ibmBorrow
		trialBal    []trialBalanceRow
	)
	if s.loanURL == "" {
		result.SkippedSources = append(result.SkippedSources, "loan-service")
	} else {
		var err error
		if agreements, err = s.fetchLoanAgreements(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("loan agreements extract: %w", err)
		}
		if collaterals, err = s.fetchLoanCollaterals(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("loan collaterals extract: %w", err)
		}
	}
	if s.depositURL == "" {
		result.SkippedSources = append(result.SkippedSources, "deposit-service")
	} else {
		var err error
		if savings, err = s.fetchDepositSavings(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("deposit extract: %w", err)
		}
		if ibmDeposits, err = s.fetchIBMDeposits(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("interbank deposit extract: %w", err)
		}
		if ibmBorrows, err = s.fetchIBMBorrows(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("interbank borrow extract: %w", err)
		}
	}
	if s.capitalURL == "" {
		result.SkippedSources = append(result.SkippedSources, "capital-service")
	} else {
		var err error
		if contracts, err = s.fetchCapitalContracts(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("capital contracts extract: %w", err)
		}
		if movements, err = s.fetchCapitalMovements(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("capital movements extract: %w", err)
		}
	}
	if s.crmURL == "" {
		result.SkippedSources = append(result.SkippedSources, "crm-service")
	} else {
		var err error
		if customers, err = s.fetchCustomers(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("customer extract: %w", err)
		}
		if members, err = s.fetchMembers(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("member extract: %w", err)
		}
		if memberReqs, err = s.fetchMemberRequests(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("member request extract: %w", err)
		}
	}
	if s.financeURL == "" {
		result.SkippedSources = append(result.SkippedSources, "finance-service")
	} else {
		var err error
		if trialBal, err = s.fetchTrialBalance(ctx, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("trial balance extract: %w", err)
		}
	}

	// Nothing wired: report the skip without opening a transaction.
	if s.loanURL == "" && s.depositURL == "" && s.capitalURL == "" && s.crmURL == "" && s.financeURL == "" {
		s.logger.Warn("report extract skipped: no sources configured", "tenant", tenantID, "business_date", businessDate)
		return result, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin extract tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if s.loanURL != "" {
		if err := replaceFacts(ctx, tx, "rpt_fact_loan_agreement_daily", tenantID, businessDate, len(agreements), func() error {
			for _, item := range agreements {
				if _, err := tx.ExecContext(ctx, insertLoanFact,
					tenantID, businessDate, item.OrgCode, item.AgreementCode, item.ContractCode,
					item.CustomerCode, item.ProductCode, nullDate(item.DisburseDate), nullDate(item.MaturityDate),
					item.DebtGroupCode, item.Status, item.CurrencyCode, item.InterestRate,
					item.DisburseAmtMinor, item.OutstandingAmtMinor, item.ProvisionAmtMinor,
					item.LoanTermMonths, item.TermBucket, item.LoanMethodCode,
					item.IndustryCode, item.PurposeCode); err != nil {
					return fmt.Errorf("insert loan fact %s: %w", item.AgreementCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.LoanAgreements = len(agreements)

		if err := replaceFacts(ctx, tx, "rpt_fact_loan_collateral_daily", tenantID, businessDate, len(collaterals), func() error {
			for _, item := range collaterals {
				if _, err := tx.ExecContext(ctx, insertCollateralFact,
					tenantID, businessDate, item.OrgCode, item.CollCode, item.CollTypeCode,
					nullDate(item.ValuationDate), item.Status, item.CollValueMinor, item.CollUseValueMinor); err != nil {
					return fmt.Errorf("insert collateral fact %s: %w", item.CollCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.LoanCollaterals = len(collaterals)
	}

	if s.depositURL != "" {
		if err := replaceFacts(ctx, tx, "rpt_fact_deposit_contract_daily", tenantID, businessDate, len(savings), func() error {
			for _, item := range savings {
				if _, err := tx.ExecContext(ctx, insertDepositFact,
					tenantID, businessDate, item.OrgCode, item.SavingsCode, item.CustomerCode,
					item.ProductCode, nullDate(item.OpenDate), nullDate(item.MaturityDate),
					item.Status, item.CurrencyCode, item.PrincipalMinor, item.AccruedMinor); err != nil {
					return fmt.Errorf("insert deposit fact %s: %w", item.SavingsCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.DepositSavings = len(savings)

		if err := replaceFacts(ctx, tx, "rpt_fact_ibm_deposit_daily", tenantID, businessDate, len(ibmDeposits), func() error {
			for _, item := range ibmDeposits {
				if _, err := tx.ExecContext(ctx, insertIBMDepositFact,
					tenantID, businessDate, item.OrgCode, item.DepositCode, item.CounterpartyCode,
					item.ProductCode, item.TermMonths, nullDate(item.DepositDate), nullDate(item.MaturityDate),
					item.Status, item.CurrencyCode, item.PrincipalMinor, item.AccruedMinor,
					item.InterestRate); err != nil {
					return fmt.Errorf("insert interbank deposit fact %s: %w", item.DepositCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.IBMDeposits = len(ibmDeposits)

		if err := replaceFacts(ctx, tx, "rpt_fact_ibm_borrow_daily", tenantID, businessDate, len(ibmBorrows), func() error {
			for _, item := range ibmBorrows {
				if _, err := tx.ExecContext(ctx, insertIBMBorrowFact,
					tenantID, businessDate, item.OrgCode, item.BorrowCode, item.CounterpartyCode,
					item.LenderType, item.FundingPurpose, item.TermMonths,
					nullDate(item.DrawdownDate), nullDate(item.MaturityDate),
					maturityStatus(item, businessDate), item.Status, item.CurrencyCode,
					item.PrincipalMinor, item.OutstandingMinor, item.AccruedMinor,
					item.InterestRate); err != nil {
					return fmt.Errorf("insert interbank borrow fact %s: %w", item.BorrowCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.IBMBorrows = len(ibmBorrows)
	}

	if s.capitalURL != "" {
		if err := replaceFacts(ctx, tx, "rpt_fact_capital_contract_daily", tenantID, businessDate, len(contracts), func() error {
			for _, item := range contracts {
				if _, err := tx.ExecContext(ctx, insertCapitalContractFact,
					tenantID, businessDate, item.OrgCode, item.ContractCode, item.FundTypeCode,
					item.CounterpartyCode, nullDate(item.ContractDate), nullDate(item.MaturityDate),
					item.AmountMinor, item.InterestRate, item.CurrencyCode, item.Status); err != nil {
					return fmt.Errorf("insert capital contract fact %s: %w", item.ContractCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.CapitalContracts = len(contracts)

		if err := replaceFacts(ctx, tx, "rpt_fact_capital_movement_daily", tenantID, businessDate, len(movements), func() error {
			for _, item := range movements {
				if _, err := tx.ExecContext(ctx, insertCapitalMovementFact,
					tenantID, businessDate, item.MovementID, item.ContractCode, item.MovementType,
					nullDate(item.MovementDate), item.AmountMinor, item.CurrencyCode, item.Status); err != nil {
					return fmt.Errorf("insert capital movement fact %s: %w", item.MovementID, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.CapitalMovements = len(movements)
	}

	if s.crmURL != "" {
		if err := replaceFacts(ctx, tx, "rpt_fact_customer_daily", tenantID, businessDate, len(customers), func() error {
			for _, item := range customers {
				if _, err := tx.ExecContext(ctx, insertCustomerFact,
					tenantID, businessDate, item.OrgCode, item.CustomerCode, item.CustomerType,
					item.Status, item.Segment, item.CustomerRank, item.RiskLevel); err != nil {
					return fmt.Errorf("insert customer fact %s: %w", item.CustomerCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.Customers = len(customers)

		if err := replaceFacts(ctx, tx, "rpt_fact_member_daily", tenantID, businessDate, len(members), func() error {
			for _, item := range members {
				if _, err := tx.ExecContext(ctx, insertMemberFact,
					tenantID, businessDate, item.OrgCode, item.MemberCode, item.CustomerCode,
					item.MemberTypeCode, item.MemberStatus, nullDate(item.OpenDate), nullDate(item.LeaveDate),
					item.EstbCapitalMinor, item.AddCapitalMinor, item.TotalCapitalMinor); err != nil {
					return fmt.Errorf("insert member fact %s: %w", item.MemberCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.Members = len(members)

		if err := replaceFacts(ctx, tx, "rpt_fact_member_request_daily", tenantID, businessDate, len(memberReqs), func() error {
			for _, item := range memberReqs {
				if _, err := tx.ExecContext(ctx, insertMemberRequestFact,
					tenantID, businessDate, item.OrgCode, memberRequestKey(item), item.MemberCode,
					item.RequestType, item.Status, item.AmountMinor, nullDate(item.RequestDate)); err != nil {
					return fmt.Errorf("insert member request fact %s: %w", item.MemberCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.MemberRequests = len(memberReqs)
	}

	if s.financeURL != "" {
		if err := replaceFacts(ctx, tx, "rpt_fact_trial_balance_daily", tenantID, businessDate, len(trialBal), func() error {
			for _, item := range trialBal {
				if _, err := tx.ExecContext(ctx, insertTrialBalanceFact,
					tenantID, businessDate, item.OrgCode, item.CoaVersion, item.AccountCode,
					item.AccountName, item.CurrencyCode,
					item.OpenDebitMinor, item.OpenCreditMinor,
					item.IncrDebitMinor, item.IncrCreditMinor,
					item.CloseDebitMinor, item.CloseCreditMinor); err != nil {
					return fmt.Errorf("insert trial balance fact %s: %w", item.AccountCode, err)
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.TrialBalance = len(trialBal)
	}

	if s.crmURL != "" {
		// Cross-fact flags must run once every source is loaded.
		if _, err := tx.ExecContext(ctx, stampCustomerFlags, tenantID, businessDate); err != nil {
			return result, fmt.Errorf("stamp customer flags: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit extract: %w", err)
	}
	s.logger.Info("report extract done",
		"tenant", tenantID, "business_date", businessDate,
		"loan_agreements", result.LoanAgreements, "loan_collaterals", result.LoanCollaterals,
		"deposit_savings", result.DepositSavings, "capital_contracts", result.CapitalContracts,
		"capital_movements", result.CapitalMovements, "customers", result.Customers,
		"members", result.Members, "member_requests", result.MemberRequests,
		"ibm_deposits", result.IBMDeposits, "trial_balance_rows", result.TrialBalance,
		"skipped", result.SkippedSources)
	return result, nil
}

// replaceFacts clears one fact table's (tenant, business_date) slice and runs
// the supplied insert loop, keeping the rebuild idempotent.
func replaceFacts(ctx context.Context, tx *sql.Tx, table, tenantID, businessDate string, _ int, insert func() error) error {
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1 AND business_date = $2::date`, table),
		tenantID, businessDate); err != nil {
		return fmt.Errorf("clear %s: %w", table, err)
	}
	return insert()
}

const insertLoanFact = `INSERT INTO rpt_fact_loan_agreement_daily
	(tenant_id, business_date, org_code, agreement_code, contract_code, customer_code, product_code,
	 disburse_date, maturity_date, debt_group_code, status, currency_code, interest_rate,
	 disburse_amt_minor, outstanding_amt_minor, provision_amt_minor,
	 loan_term_months, term_bucket, loan_method_code, industry_code, purpose_code)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8::date, $9::date, $10, $11, $12, $13, $14, $15, $16,
	        $17, $18, $19, $20, $21)`

const insertCollateralFact = `INSERT INTO rpt_fact_loan_collateral_daily
	(tenant_id, business_date, org_code, coll_code, coll_type_code, valuation_date, status,
	 coll_value_minor, coll_use_value_minor)
	VALUES ($1, $2::date, $3, $4, $5, $6::date, $7, $8, $9)`

const insertDepositFact = `INSERT INTO rpt_fact_deposit_contract_daily
	(tenant_id, business_date, org_code, savings_code, customer_code, product_code,
	 open_date, maturity_date, status, currency_code, principal_minor, accrued_minor)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7::date, $8::date, $9, $10, $11, $12)`

const insertCapitalContractFact = `INSERT INTO rpt_fact_capital_contract_daily
	(tenant_id, business_date, org_code, contract_code, fund_type_code, counterparty_code,
	 contract_date, maturity_date, amount_minor, interest_rate, currency_code, status)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7::date, $8::date, $9, $10, $11, $12)`

const insertCapitalMovementFact = `INSERT INTO rpt_fact_capital_movement_daily
	(tenant_id, business_date, movement_id, contract_code, movement_type, movement_date,
	 amount_minor, currency_code, status)
	VALUES ($1, $2::date, $3, $4, $5, $6::date, $7, $8, $9)`

const insertCustomerFact = `INSERT INTO rpt_fact_customer_daily
	(tenant_id, business_date, org_code, customer_code, customer_type, status,
	 segment, customer_rank, risk_level)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9)`

// stampCustomerFlags sets the cross-fact predicates the customer counts need
// (member / has a deposit / has a loan). It runs after every source is inserted
// so it sees the whole tenant slice for the business date.
const stampCustomerFlags = `
	UPDATE rpt_fact_customer_daily c
	SET is_member = CASE WHEN EXISTS (
	        SELECT 1 FROM rpt_fact_member_daily m
	        WHERE m.tenant_id = c.tenant_id AND m.business_date = c.business_date
	          AND m.customer_code = c.customer_code AND m.member_status = 'ACTIVE')
	      THEN 'Y' ELSE 'N' END,
	    has_deposit = CASE WHEN EXISTS (
	        SELECT 1 FROM rpt_fact_deposit_contract_daily d
	        WHERE d.tenant_id = c.tenant_id AND d.business_date = c.business_date
	          AND d.customer_code = c.customer_code AND d.status = 'ACTIVE'
	          AND d.principal_minor > 0)
	      THEN 'Y' ELSE 'N' END,
	    has_loan = CASE WHEN EXISTS (
	        SELECT 1 FROM rpt_fact_loan_agreement_daily l
	        WHERE l.tenant_id = c.tenant_id AND l.business_date = c.business_date
	          AND l.customer_code = c.customer_code AND l.status = 'ACTIVE'
	          AND l.outstanding_amt_minor > 0)
	      THEN 'Y' ELSE 'N' END
	WHERE c.tenant_id = $1 AND c.business_date = $2::date`

const insertMemberFact = `INSERT INTO rpt_fact_member_daily
	(tenant_id, business_date, org_code, member_code, customer_code, member_type_code,
	 member_status, open_date, leave_date, estb_capital_minor, add_capital_minor, total_capital_minor)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8::date, $9::date, $10, $11, $12)`

const insertMemberRequestFact = `INSERT INTO rpt_fact_member_request_daily
	(tenant_id, business_date, org_code, request_key, member_code, request_type,
	 status, amount_minor, request_date)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9::date)`

const insertIBMDepositFact = `INSERT INTO rpt_fact_ibm_deposit_daily
	(tenant_id, business_date, org_code, deposit_code, counterparty_code, product_code,
	 term_months, deposit_date, maturity_date, status, currency_code,
	 principal_minor, accrued_minor, interest_rate)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8::date, $9::date, $10, $11, $12, $13, $14)`

const insertIBMBorrowFact = `INSERT INTO rpt_fact_ibm_borrow_daily
	(tenant_id, business_date, org_code, borrow_code, counterparty_code, lender_type,
	 funding_purpose, term_months, drawdown_date, maturity_date, maturity_status,
	 status, currency_code, principal_minor, outstanding_minor, accrued_minor, interest_rate)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9::date, $10::date, $11, $12, $13, $14, $15, $16, $17)`

// maturityStatus classifies a borrow against the snapshot date: a matured but
// still outstanding ACTIVE borrow is OVERDUE, a retired one is SETTLED,
// everything else is CURRENT. The "nợ trong hạn / quá hạn" indicators read it.
func maturityStatus(item ibmBorrow, businessDate string) string {
	if item.Status != "ACTIVE" {
		return "SETTLED"
	}
	if item.MaturityDate != "" && item.MaturityDate < businessDate {
		return "OVERDUE"
	}
	return "CURRENT"
}

const insertTrialBalanceFact = `INSERT INTO rpt_fact_trial_balance_daily
	(tenant_id, business_date, org_code, coa_version, account_code, account_name, currency_code,
	 open_debit_minor, open_credit_minor, incr_debit_minor, incr_credit_minor,
	 close_debit_minor, close_credit_minor)
	VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

// memberRequestKey is the stable per-request identity inside a daily fact. Use
// the source request id: (member, type, date) is NOT unique — a member may make
// several additional-capital requests on the same day, which collided on the
// fact primary key and failed the whole extract.
func memberRequestKey(item memberRequest) string {
	if item.RequestID != "" {
		return item.RequestID
	}
	return item.MemberCode + ":" + item.RequestType + ":" + item.RequestDate
}

func nullDate(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// ── signed fetch ──

func (s *Service) fetchLoanAgreements(ctx context.Context, tenantID, businessDate string) ([]loanAgreement, error) {
	var env envelope[loanAgreementResult]
	if err := s.fetch(ctx, s.loanURL, "loan-service", "/internal/reporting/loan-agreements", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchLoanCollaterals(ctx context.Context, tenantID, businessDate string) ([]loanCollateral, error) {
	var env envelope[collateralResult]
	if err := s.fetch(ctx, s.loanURL, "loan-service", "/internal/reporting/loan-collaterals", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchDepositSavings(ctx context.Context, tenantID, businessDate string) ([]depositSavings, error) {
	var env envelope[depositSavingsResult]
	if err := s.fetch(ctx, s.depositURL, "deposit-service", "/internal/reporting/deposit-savings", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchCapitalContracts(ctx context.Context, tenantID, businessDate string) ([]capitalContract, error) {
	var env envelope[capitalContractResult]
	if err := s.fetch(ctx, s.capitalURL, "capital-service", "/internal/reporting/capital-contracts", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchCapitalMovements(ctx context.Context, tenantID, businessDate string) ([]capitalMovement, error) {
	var env envelope[capitalMovementResult]
	if err := s.fetch(ctx, s.capitalURL, "capital-service", "/internal/reporting/capital-movements", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchCustomers(ctx context.Context, tenantID, businessDate string) ([]customer, error) {
	var env envelope[customerResult]
	if err := s.fetch(ctx, s.crmURL, "crm-service", "/internal/reporting/customers", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchMembers(ctx context.Context, tenantID, businessDate string) ([]member, error) {
	var env envelope[memberResult]
	if err := s.fetch(ctx, s.crmURL, "crm-service", "/internal/reporting/members", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchMemberRequests(ctx context.Context, tenantID, businessDate string) ([]memberRequest, error) {
	var env envelope[memberRequestResult]
	if err := s.fetch(ctx, s.crmURL, "crm-service", "/internal/reporting/member-requests", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchIBMDeposits(ctx context.Context, tenantID, businessDate string) ([]ibmDeposit, error) {
	var env envelope[ibmDepositResult]
	if err := s.fetch(ctx, s.depositURL, "deposit-service", "/internal/reporting/ibm-deposits", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchIBMBorrows(ctx context.Context, tenantID, businessDate string) ([]ibmBorrow, error) {
	var env envelope[ibmBorrowResult]
	if err := s.fetch(ctx, s.depositURL, "deposit-service", "/internal/reporting/ibm-borrows", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

func (s *Service) fetchTrialBalance(ctx context.Context, tenantID, businessDate string) ([]trialBalanceRow, error) {
	var env envelope[trialBalanceResult]
	if err := s.fetch(ctx, s.financeURL, "finance-service", "/internal/reporting/trial-balance", tenantID, businessDate, &env); err != nil {
		return nil, err
	}
	return env.Result.Items, nil
}

// fetch performs a signed GET against a domain service's reporting surface and
// decodes the canonical success envelope. The caller identity is derived
// solely from source + audience; the delegated tenant travels in X-Tenant-Id,
// never in a query string.
func (s *Service) fetch(ctx context.Context, baseURL, audience, path, tenantID, businessDate string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path+"?as_of="+businessDate, nil)
	if err != nil {
		return fmt.Errorf("create %s request: %w", audience, err)
	}
	req.Header.Set("X-Tenant-Id", tenantID)
	req.Header.Set("X-User-Id", "eod-job")
	if err := identity.SignRequest(req, s.secret, "statistical-service", audience, time.Now(), signTTL); err != nil {
		return fmt.Errorf("sign %s request: %w", audience, err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", audience, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s returned status %d: %s", audience, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, extractMaxResponse+1))
	if err != nil {
		return fmt.Errorf("read %s response: %w", audience, err)
	}
	if int64(len(body)) > extractMaxResponse {
		return fmt.Errorf("%s response exceeds %d bytes", audience, extractMaxResponse)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s response: %w", audience, err)
	}
	return nil
}

type envelope[T any] struct {
	Result  T    `json:"result"`
	Success bool `json:"success"`
}

type loanAgreementResult struct {
	AsOf  string          `json:"as_of"`
	Items []loanAgreement `json:"items"`
}

type loanAgreement struct {
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
	LoanMethodCode      string  `json:"loan_method_code"`
	IndustryCode        string  `json:"industry_code"`
	PurposeCode         string  `json:"purpose_code"`
	LoanTermMonths      int     `json:"loan_term_months"`
	TermBucket          string  `json:"term_bucket"`
}

type collateralResult struct {
	AsOf  string           `json:"as_of"`
	Items []loanCollateral `json:"items"`
}

type loanCollateral struct {
	CollCode          string `json:"coll_code"`
	CollTypeCode      string `json:"coll_type_code"`
	OrgCode           string `json:"org_code"`
	ValuationDate     string `json:"valuation_date"`
	Status            string `json:"status"`
	CollValueMinor    int64  `json:"coll_value_minor"`
	CollUseValueMinor int64  `json:"coll_use_value_minor"`
}

type depositSavingsResult struct {
	AsOf  string           `json:"as_of"`
	Items []depositSavings `json:"items"`
}

type depositSavings struct {
	SavingsCode    string `json:"savings_code"`
	CustomerCode   string `json:"customer_code"`
	ProductCode    string `json:"product_code"`
	OrgCode        string `json:"org_code"`
	OpenDate       string `json:"open_date"`
	MaturityDate   string `json:"maturity_date"`
	PrincipalMinor int64  `json:"principal_minor"`
	AccruedMinor   int64  `json:"accrued_minor"`
	CurrencyCode   string `json:"currency_code"`
	Status         string `json:"status"`
}

type capitalContractResult struct {
	AsOf  string            `json:"as_of"`
	Items []capitalContract `json:"items"`
}

type capitalContract struct {
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

type capitalMovementResult struct {
	AsOf  string            `json:"as_of"`
	Items []capitalMovement `json:"items"`
}

type capitalMovement struct {
	MovementID   string `json:"movement_id"`
	ContractCode string `json:"contract_code"`
	MovementType string `json:"movement_type"`
	MovementDate string `json:"movement_date"`
	AmountMinor  int64  `json:"amount_minor"`
	CurrencyCode string `json:"currency_code"`
	Status       string `json:"status"`
}

type customerResult struct {
	AsOf  string     `json:"as_of"`
	Items []customer `json:"items"`
}

type customer struct {
	CustomerCode string `json:"customer_code"`
	OrgCode      string `json:"org_code"`
	CustomerType string `json:"customer_type"`
	Status       string `json:"status"`
	Segment      string `json:"segment"`
	CustomerRank string `json:"customer_rank"`
	RiskLevel    string `json:"risk_level"`
}

type memberResult struct {
	AsOf  string   `json:"as_of"`
	Items []member `json:"items"`
}

type member struct {
	MemberCode        string `json:"member_code"`
	CustomerCode      string `json:"customer_code"`
	OrgCode           string `json:"org_code"`
	MemberTypeCode    string `json:"member_type_code"`
	MemberStatus      string `json:"member_status"`
	OpenDate          string `json:"open_date"`
	LeaveDate         string `json:"leave_date"`
	EstbCapitalMinor  int64  `json:"estb_capital_minor"`
	AddCapitalMinor   int64  `json:"add_capital_minor"`
	TotalCapitalMinor int64  `json:"total_capital_minor"`
}

type memberRequestResult struct {
	AsOf  string          `json:"as_of"`
	Items []memberRequest `json:"items"`
}

type memberRequest struct {
	RequestID   string `json:"request_id"`
	MemberCode  string `json:"member_code"`
	OrgCode     string `json:"org_code"`
	RequestType string `json:"request_type"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
	RequestDate string `json:"request_date"`
}

type ibmDepositResult struct {
	AsOf  string       `json:"as_of"`
	Items []ibmDeposit `json:"items"`
}

// ibmDeposit is one interbank deposit contract (PCF topic "Tiền gửi TCTD").
type ibmDeposit struct {
	DepositCode      string  `json:"deposit_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	ProductCode      string  `json:"product_code"`
	TermMonths       int     `json:"term_months"`
	DepositDate      string  `json:"deposit_date"`
	MaturityDate     string  `json:"maturity_date"`
	PrincipalMinor   int64   `json:"principal_minor"`
	AccruedMinor     int64   `json:"accrued_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	OrgCode          string  `json:"org_code"`
	Status           string  `json:"status"`
}

type ibmBorrowResult struct {
	AsOf  string      `json:"as_of"`
	Items []ibmBorrow `json:"items"`
}

// ibmBorrow is one interbank borrowing contract (PCF topic "Tiền vay TCTD").
type ibmBorrow struct {
	BorrowCode       string  `json:"borrow_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	LenderType       string  `json:"lender_type"`
	FundingPurpose   string  `json:"funding_purpose"`
	TermMonths       int     `json:"term_months"`
	DrawdownDate     string  `json:"drawdown_date"`
	MaturityDate     string  `json:"maturity_date"`
	PrincipalMinor   int64   `json:"principal_minor"`
	OutstandingMinor int64   `json:"outstanding_minor"`
	AccruedMinor     int64   `json:"accrued_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	OrgCode          string  `json:"org_code"`
	Status           string  `json:"status"`
}

type trialBalanceResult struct {
	AsOf  string            `json:"as_of"`
	Items []trialBalanceRow `json:"items"`
}

// trialBalanceRow is one account's daily balance (PCF "Tài chính kế toán").
type trialBalanceRow struct {
	OrgCode          string `json:"org_code"`
	CoaVersion       string `json:"coa_version"`
	AccountCode      string `json:"account_code"`
	AccountName      string `json:"account_name"`
	CurrencyCode     string `json:"currency_code"`
	OpenDebitMinor   int64  `json:"open_debit_minor"`
	OpenCreditMinor  int64  `json:"open_credit_minor"`
	IncrDebitMinor   int64  `json:"incr_debit_minor"`
	IncrCreditMinor  int64  `json:"incr_credit_minor"`
	CloseDebitMinor  int64  `json:"close_debit_minor"`
	CloseCreditMinor int64  `json:"close_credit_minor"`
}
