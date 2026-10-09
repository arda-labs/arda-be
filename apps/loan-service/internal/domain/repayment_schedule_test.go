package domain

import "testing"

func TestBuildEvenPrincipalPlans(t *testing.T) {
	tests := []struct {
		name      string
		agreement Agreement
		terms     int
		start     string
		wantErr   bool
	}{
		{"valid", Agreement{ContractCode: "C1", AgreementCode: "A1", CurrencyCode: "VND", OutstandingAmt: 1_000_000_000, InterestRate: 12}, 7, "2026-10-01", false},
		{"zero terms", Agreement{CurrencyCode: "VND", OutstandingAmt: 100}, 0, "2026-10-01", true},
		{"invalid start", Agreement{CurrencyCode: "VND", OutstandingAmt: 100}, 3, "not-a-date", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plans, err := BuildEvenPrincipalPlans(tt.agreement, tt.terms, tt.start)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BuildEvenPrincipalPlans() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(plans) != tt.terms {
				t.Fatalf("got %d plans, want %d", len(plans), tt.terms)
			}
			var principalTotal int64
			for i, plan := range plans {
				principalTotal += plan.PlanPrincipalAmt
				if plan.TermNo != i+1 || plan.PlanNo != 1 || plan.AgreementCode != tt.agreement.AgreementCode {
					t.Fatalf("plan %d identity mismatch: %+v", i, plan)
				}
				if plan.PlanPrincipalAmt <= 0 || plan.PlanInterestAmt <= 0 {
					t.Fatalf("plan %d amounts must be positive: %+v", i, plan)
				}
				if i > 0 && plan.PlanInterestAmt > plans[i-1].PlanInterestAmt {
					t.Fatalf("declining balance interest increased at term %d", plan.TermNo)
				}
			}
			if principalTotal != tt.agreement.OutstandingAmt {
				t.Fatalf("principal total = %d, want %d", principalTotal, tt.agreement.OutstandingAmt)
			}
		})
	}
}
