package bootstrap

import _ "embed"

//go:embed customer-registration-v1.bpmn
var customerRegistrationV1 []byte

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
	}
}
