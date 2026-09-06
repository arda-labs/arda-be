package bootstrap

import _ "embed"

//go:embed lnm-debt-change-v2.bpmn
var lnmDebtChange []byte

//go:embed lnm-rate-change-v2.bpmn
var lnmRateChange []byte

//go:embed lnm-restructure-v2.bpmn
var lnmRestructure []byte

//go:embed lnm-waiver-v2.bpmn
var lnmWaiver []byte

//go:embed lnm-writeoff-v2.bpmn
var lnmWriteoff []byte

//go:embed lnm-recovery-v2.bpmn
var lnmRecovery []byte

//go:embed lnm-fund-check-v2.bpmn
var lnmFundCheck []byte

//go:embed lnm-revenue-allocation-v2.bpmn
var lnmRevenueAllocation []byte

//go:embed lnm-vfu-fee-allocation-v2.bpmn
var lnmVfuFeeAllocation []byte

//go:embed lnm-off-balance-export-v2.bpmn
var lnmOffBalanceExport []byte

//go:embed crm-customer-registration-v2.bpmn
var crmCustomerRegistrationV2 []byte

//go:embed customer-adjustment-v2.bpmn
var customerAdjustmentV2 []byte

//go:embed lnm-loan-formation-v2.bpmn
var lnmLoanFormationV2 []byte

//go:embed lnm-disbursement-v2.bpmn
var lnmDisbursement []byte

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
			ProcessCode:  "LNM_DEBT_CHANGE_V2",
			Name:         "Chuyển nhóm nợ (v2)",
			ResourceName: "lnm-debt-change-v2.bpmn",
			Content:      lnmDebtChange,
		},
		{
			ProcessCode:  "LNM_RATE_CHANGE_V2",
			Name:         "Thay đổi lãi suất (v2)",
			ResourceName: "lnm-rate-change-v2.bpmn",
			Content:      lnmRateChange,
		},
		{
			ProcessCode:  "LNM_RESTRUCTURE_V2",
			Name:         "Gia hạn nợ (v2)",
			ResourceName: "lnm-restructure-v2.bpmn",
			Content:      lnmRestructure,
		},
		{
			ProcessCode:  "LNM_WAIVER_V2",
			Name:         "Miễn giảm lãi (v2)",
			ResourceName: "lnm-waiver-v2.bpmn",
			Content:      lnmWaiver,
		},
		{
			ProcessCode:  "LNM_WRITEOFF_V2",
			Name:         "Xử lý nợ (v2)",
			ResourceName: "lnm-writeoff-v2.bpmn",
			Content:      lnmWriteoff,
		},
		{
			ProcessCode:  "LNM_RECOVERY_V2",
			Name:         "Thu hồi nợ (v2)",
			ResourceName: "lnm-recovery-v2.bpmn",
			Content:      lnmRecovery,
		},
		{
			ProcessCode:  "LNM_FUND_CHECK_V2",
			Name:         "Kiểm tra sử dụng vốn (v2)",
			ResourceName: "lnm-fund-check-v2.bpmn",
			Content:      lnmFundCheck,
		},
		{
			ProcessCode:  "LNM_REVENUE_ALLOCATION_V2",
			Name:         "Phân bổ doanh thu (v2)",
			ResourceName: "lnm-revenue-allocation-v2.bpmn",
			Content:      lnmRevenueAllocation,
		},
		{
			ProcessCode:  "LNM_VFU_FEE_ALLOCATION_V2",
			Name:         "Trích phí ủy thác (v2)",
			ResourceName: "lnm-vfu-fee-allocation-v2.bpmn",
			Content:      lnmVfuFeeAllocation,
		},
		{
			ProcessCode:  "LNM_OFF_BALANCE_EXPORT_V2",
			Name:         "Xuất toán ngoại bảng (v2)",
			ResourceName: "lnm-off-balance-export-v2.bpmn",
			Content:      lnmOffBalanceExport,
		},
		{
			ProcessCode:  "LNM_DISBURSEMENT_V2",
			Name:         "Giải ngân (v2)",
			ResourceName: "lnm-disbursement-v2.bpmn",
			Content:      lnmDisbursement,
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
