package domain

import "testing"

func TestContractEditable(t *testing.T) {
	for _, tt := range []struct {
		status string
		want   bool
	}{{ContractDraft, true}, {ContractPendingApproval, true}, {ContractRejected, true}, {ContractDisbursed, false}, {ContractClosed, false}, {"", false}} {
		if got := ContractEditable(tt.status); got != tt.want {
			t.Errorf("ContractEditable(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestValidateContractUpdate(t *testing.T) {
	valid := &Contract{LoanAmt: 500_000_000, InterestRate: 8.5, LoanTerm: 12}
	if err := ValidateContractUpdate(valid); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		in   *Contract
	}{
		{"loan amount", &Contract{InterestRate: 8.5, LoanTerm: 12}},
		{"interest rate", &Contract{LoanAmt: 1, LoanTerm: 12}},
		{"term", &Contract{LoanAmt: 1, InterestRate: 8.5}},
		{"contract date", &Contract{LoanAmt: 1, InterestRate: 8.5, LoanTerm: 12, ContractDate: "07/09/2026"}},
		{"maturity date", &Contract{LoanAmt: 1, InterestRate: 8.5, LoanTerm: 12, MaturityDate: "2026-13-40"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateContractUpdate(tc.in); err == nil {
				t.Fatal("invalid payload accepted")
			}
		})
	}
	withDates := &Contract{LoanAmt: 1, InterestRate: 8.5, LoanTerm: 12, ContractDate: "2026-09-07", MaturityDate: "2027-09-07"}
	if err := ValidateContractUpdate(withDates); err != nil {
		t.Fatalf("valid dates rejected: %v", err)
	}
}
