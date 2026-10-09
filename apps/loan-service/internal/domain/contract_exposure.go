package domain

// ContractExposure is the single model for consuming a contract's approved
// loan amount. Outstanding is principal already disbursed and still owed;
// pending is registered principal waiting for its COMPLETE transfer; held
// reservations are requests currently being processed but not yet posted.
// These amounts are disjoint and therefore add to the contract exposure.
type ContractExposure struct {
	LoanAmountMinor  int64
	OutstandingMinor int64
	PendingMinor     int64
	ReservedMinor    int64
}

// UsedMinor returns the contract limit already consumed or held.
func (e ContractExposure) UsedMinor() int64 {
	return e.OutstandingMinor + e.PendingMinor + e.ReservedMinor
}

// HeadroomMinor returns the remaining amount available for a REGISTER.
func (e ContractExposure) HeadroomMinor() int64 {
	return e.LoanAmountMinor - e.UsedMinor()
}

// Allows reports whether amountMinor fits the remaining contract limit.
func (e ContractExposure) Allows(amountMinor int64) bool {
	return amountMinor > 0 && amountMinor <= e.HeadroomMinor()
}
