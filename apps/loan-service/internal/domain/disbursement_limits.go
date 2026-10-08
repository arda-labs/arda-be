package domain

import "fmt"

// CheckCompleteRemainder ensures completions cannot exceed the source register.
func CheckCompleteRemainder(sourceRegisterMinor, completedMinor, disbursementMinor int64) error {
	if disbursementMinor > sourceRegisterMinor-completedMinor {
		return fmt.Errorf("disburse_amt_minor %d exceeds source remainder %d (register %d - completed %d)",
			disbursementMinor, sourceRegisterMinor-completedMinor, sourceRegisterMinor, completedMinor)
	}
	return nil
}

// CheckRegisterLimit ensures a register disbursement fits the contract headroom.
func CheckRegisterLimit(contractLoanMinor, contractOutstandingMinor, disbursementMinor int64) error {
	exposure := ContractExposure{LoanAmountMinor: contractLoanMinor, OutstandingMinor: contractOutstandingMinor}
	if !exposure.Allows(disbursementMinor) {
		return fmt.Errorf("disburse_amt_minor %d exceeds contract headroom %d", disbursementMinor, exposure.HeadroomMinor())
	}
	return nil
}
