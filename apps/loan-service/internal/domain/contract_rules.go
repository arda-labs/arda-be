package domain

import (
	"fmt"
	"time"
)

// ContractEditable reports whether maker revision remains allowed by status.
func ContractEditable(status string) bool {
	return status == ContractDraft || status == ContractPendingApproval || status == ContractRejected
}

// ValidateContractUpdate validates business fields accepted by maker revision.
func ValidateContractUpdate(in *Contract) error {
	if in.LoanAmt <= 0 {
		return fmt.Errorf("loan_amt must be positive")
	}
	if in.InterestRate <= 0 {
		return fmt.Errorf("interest_rate must be positive")
	}
	if in.LoanTerm <= 0 {
		return fmt.Errorf("loan_term must be positive")
	}
	for field, date := range map[string]string{"contract_date": in.ContractDate, "maturity_date": in.MaturityDate} {
		if date == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("%s must be a valid YYYY-MM-DD date", field)
		}
	}
	return nil
}
