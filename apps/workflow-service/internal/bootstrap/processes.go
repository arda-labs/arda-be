package bootstrap

import _ "embed"

//go:embed customer-registration-v1.bpmn
var customerRegistrationV1 []byte

//go:embed customer-adjustment-v1.bpmn
var customerAdjustmentV1 []byte

//go:embed hrm-employee-registration-v1.bpmn
var hrmEmployeeRegistrationV1 []byte

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
			ResourceName: "customer-registration-v1.bpmn",
			Content:      customerRegistrationV1,
		},
		{
			ProcessCode:  "CUSTOMER_ADJUSTMENT",
			Name:         "Điều chỉnh hồ sơ khách hàng",
			ResourceName: "customer-adjustment-v1.bpmn",
			Content:      customerAdjustmentV1,
		},
		{
			ProcessCode:  "HRM_EMPLOYEE_REGISTRATION",
			Name:         "Dang ky nhan su",
			ResourceName: "hrm-employee-registration-v1.bpmn",
			Content:      hrmEmployeeRegistrationV1,
		},
	}
}
