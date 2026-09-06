package bootstrap

import _ "embed"

//go:embed crm-customer-registration-v2.bpmn
var crmCustomerRegistrationV2 []byte

//go:embed customer-adjustment-v2.bpmn
var customerAdjustmentV2 []byte

//go:embed lnm-loan-formation-v2.bpmn
var lnmLoanFormationV2 []byte

type Process struct {
	ProcessCode  string
	Name         string
	ResourceName string
	Content      []byte
}

func BuiltInProcesses() []Process {
	return []Process{
		{
			ProcessCode:  "CUSTOMER_REGISTRATION",
			Name:         "Đăng ký khách hàng hội viên",
			ResourceName: "crm-customer-registration-v2.bpmn",
			Content:      crmCustomerRegistrationV2,
		},
		{
			ProcessCode:  "CUSTOMER_ADJUSTMENT",
			Name:         "Điều chỉnh hồ sơ khách hàng",
			ResourceName: "customer-adjustment-v2.bpmn",
			Content:      customerAdjustmentV2,
		},
		{
			// Multi-level approval sample derived from EPAS LNM.201.01 —
			// proves the platform handles tiered review before the loan
			// domain lands (P1). Reference flow only, no domain worker yet.
			ProcessCode:  "LOAN_FORMATION_V2",
			Name:         "Hình thành khoản vay đa cấp (mẫu LNM.201.01)",
			ResourceName: "lnm-loan-formation-v2.bpmn",
			Content:      lnmLoanFormationV2,
		},
	}
}
