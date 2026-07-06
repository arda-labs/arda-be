package bootstrap

import _ "embed"

//go:embed crm-customer-registration-v2.bpmn
var crmCustomerRegistrationV2 []byte

//go:embed customer-adjustment-v2.bpmn
var customerAdjustmentV2 []byte

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
	}
}
