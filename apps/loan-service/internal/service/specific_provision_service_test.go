package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

const specificProvisionTenantID = "00000000-0000-0000-0000-000000000031"

func openSpecificProvisionFixture(t *testing.T, debtGroup, contractStatus string) (*sql.DB, *repository.LoanRepository, string, string) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	t.Cleanup(func() { _ = db.Close() })
	repo := repository.NewLoanRepository(db)
	run := time.Now().UTC().Format("20060102T150405.000000000")
	contractCode, agreementCode := "SP-C-"+run, "SP-A-"+run
	ctx := context.Background()
	if _, err := repo.CreateContract(ctx, &domain.Contract{
		ID: repository.NewID("ctrt"), TenantID: specificProvisionTenantID, ContractCode: contractCode,
		CustomerCode: "SP-CUST", LoanAmt: 1_000_000, LoanTerm: 12, TermUnit: "MONTH",
		ContractDate: "2026-10-01", MaturityDate: "2027-10-01", Status: contractStatus, CreatedBy: "test",
	}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := repo.CreateAgreement(ctx, &domain.Agreement{
		ID: repository.NewID("agr"), TenantID: specificProvisionTenantID, ContractCode: contractCode,
		AgreementCode: agreementCode, DisburseDate: "2026-10-01", LoanTerm: 12,
		TermUnit: "MONTH", MaturityDate: "2027-10-01", DebtGroupCode: debtGroup,
		Status: "ACTIVE", CreatedBy: "test",
	}); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	if _, err := db.Exec(`UPDATE lnm_agreements SET outstanding_amt_minor = 100000, provision_amt_minor = 20000 WHERE tenant_id = $1 AND agreement_code = $2`, specificProvisionTenantID, agreementCode); err != nil {
		t.Fatalf("seed agreement balances: %v", err)
	}
	return db, repo, contractCode, agreementCode
}

func TestSpecificProvisionMissingRateHasExplicitError(t *testing.T) {
	db, repo, _, agreementCode := openSpecificProvisionFixture(t, "GROUP_UNCONFIRMED", "ACTIVE")
	svc := NewSpecificProvisionService(repo, nil, nil)
	_, err := svc.Calculate(context.Background(), specificProvisionTenantID, agreementCode, "2026-10-08")
	var appErr *ardaerrors.Error
	if !errors.As(err, &appErr) || appErr.Code != "PROVISION_RATE_MISSING" {
		t.Fatalf("Calculate error = %v, want PROVISION_RATE_MISSING", err)
	}
	_ = db
}

func TestSpecificProvisionClosedContractRejected(t *testing.T) {
	_, repo, _, agreementCode := openSpecificProvisionFixture(t, "GROUP_3", "CLOSED")
	svc := NewSpecificProvisionService(repo, nil, nil)
	_, err := svc.Calculate(context.Background(), specificProvisionTenantID, agreementCode, "2026-10-08")
	if err == nil {
		t.Fatal("Calculate on a CLOSED contract should fail")
	}
}

func TestSpecificProvisionResolveReplayDoesNotApplyTwice(t *testing.T) {
	db, repo, contractCode, agreementCode := openSpecificProvisionFixture(t, "GROUP_3", "ACTIVE")
	ctx := context.Background()
	// outstanding 100000 × 20% = 20000, already provisioned; approval has no posting delta.
	row := repository.SpecificProvisionRow{
		ID: repository.NewID("sp"), TenantID: specificProvisionTenantID, ContractCode: contractCode,
		AgreementCode: agreementCode, ProvisionDate: "2026-10-08", OutstandingMinor: 100000,
		DebtGroupCode: "GROUP_3", RatePercent: 20, BaseMinor: 100000, AmountMinor: 20000,
		Status: SpecificProvisionStatusPending, CreatedBy: "maker",
	}
	if err := repo.InsertSpecificProvision(ctx, row); err != nil {
		t.Fatalf("insert provision: %v", err)
	}
	svc := NewSpecificProvisionService(repo, nil, nil)
	if err := svc.Resolve(ctx, specificProvisionTenantID, row.ID, "APPROVE", "checker", ""); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if err := svc.Resolve(ctx, specificProvisionTenantID, row.ID, "APPROVE", "checker", ""); err == nil {
		t.Fatal("replayed resolve should be rejected")
	}
	var provisioned int64
	if err := db.QueryRow(`SELECT provision_amt_minor FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`, specificProvisionTenantID, agreementCode).Scan(&provisioned); err != nil {
		t.Fatalf("read provision balance: %v", err)
	}
	if provisioned != 20000 {
		t.Fatalf("provision_amt_minor = %d, want 20000 after replay", provisioned)
	}
}

func TestSpecificProvisionSettlePersistsNewAgreementBalance(t *testing.T) {
	for _, required := range []int64{35000, 5000} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			db, repo, contractCode, agreementCode := openSpecificProvisionFixture(t, "GROUP_3", "ACTIVE")
			ctx := context.Background()
			row := repository.SpecificProvisionRow{
				ID: repository.NewID("sp"), TenantID: specificProvisionTenantID, ContractCode: contractCode,
				AgreementCode: agreementCode, ProvisionDate: "2026-10-08", OutstandingMinor: 100000,
				DebtGroupCode: "GROUP_3", RatePercent: 20, BaseMinor: 100000, AmountMinor: 20000,
				Status: SpecificProvisionStatusPending, CreatedBy: "maker",
			}
			if err := repo.InsertSpecificProvision(ctx, row); err != nil {
				t.Fatalf("insert provision: %v", err)
			}
			var postedDelta int64
			if err := repo.SettleSpecificProvision(ctx, specificProvisionTenantID, row.ID, required, "checker", func(delta int64) (string, error) {
				postedDelta = delta
				return "", nil
			}); err != nil {
				t.Fatalf("settle provision: %v", err)
			}
			var balance int64
			if err := db.QueryRow(`SELECT provision_amt_minor FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`, specificProvisionTenantID, agreementCode).Scan(&balance); err != nil {
				t.Fatalf("read provision balance: %v", err)
			}
			settled, err := repo.GetSpecificProvision(ctx, specificProvisionTenantID, row.ID)
			if err != nil {
				t.Fatalf("read settled row: %v", err)
			}
			if postedDelta != required-20000 || balance != required || settled.AmountMinor != required {
				t.Fatalf("delta/balance/row amount = %d/%d/%d, want %d/%d/%d", postedDelta, balance, settled.AmountMinor, required-20000, required, required)
			}
		})
	}
}

func TestCreateCollateralRequiresExplicitDeductionRatio(t *testing.T) {
	svc := NewLoanService(nil, nil)
	_, err := svc.CreateCollateral(context.Background(), specificProvisionTenantID, "maker", &domain.Collateral{CollCode: "SP-COLL"})
	var appErr *ardaerrors.Error
	if !errors.As(err, &appErr) || appErr.Code != ardaerrors.CodeRequired {
		t.Fatalf("CreateCollateral error = %v, want required deduction_ratio", err)
	}
}

func TestSpecificProvisionDeductionRatioIsPercentage(t *testing.T) {
	db, repo, contractCode, agreementCode := openSpecificProvisionFixture(t, "GROUP_3", "ACTIVE")
	collateralCode := "SP-COLL-" + fmt.Sprint(time.Now().UnixNano())
	if _, err := db.Exec(`
		INSERT INTO lnm_collaterals (id, tenant_id, coll_code, coll_name, coll_type_code, coll_value_minor, deduction_ratio)
		VALUES ($1,$2,$3,'test collateral','TEST',100000,25)`, repository.NewID("coll"), specificProvisionTenantID, collateralCode); err != nil {
		t.Fatalf("seed collateral: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO lnm_contract_collaterals (id, tenant_id, contract_code, coll_code, coll_value_minor) VALUES ($1,$2,$3,$4,100000)`, repository.NewID("ccoll"), specificProvisionTenantID, contractCode, collateralCode); err != nil {
		t.Fatalf("link collateral: %v", err)
	}
	svc := NewSpecificProvisionService(repo, nil, nil)
	preview, err := svc.Calculate(context.Background(), specificProvisionTenantID, agreementCode, "2026-10-08")
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if preview.DeductionMinor != 25000 {
		t.Fatalf("deduction = %d, want 25000 (25%% of 100000)", preview.DeductionMinor)
	}
}
