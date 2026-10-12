package config

import "strings"

// GRPCAddrFor returns the configured gRPC address of an owner service named in
// business_operation_types.owner_service, or "" when this deployment has no
// address for it. The shared maker-checker dispatcher (ADR-011) uses it to reach
// the service that implements CaseCommandService for a flow kind.
func (c Config) GRPCAddrFor(service string) string {
	switch strings.TrimSpace(service) {
	case "capital-service":
		return c.CapitalGRPCAddr
	case "crm-service":
		return c.CRMGRPCAddr
	case "deposit-service":
		return c.DepositGRPCAddr
	case "finance-service":
		return c.FinanceGRPCAddr
	case "hrm-service":
		return c.HRMGRPCAddr
	case "iam-service":
		return c.IAMGRPCAddr
	case "loan-service":
		return c.LoanGRPCAddr
	case "notification-service":
		return c.NotificationGRPCAddr
	case "statistical-service":
		return c.StatisticalGRPCAddr
	default:
		return ""
	}
}
