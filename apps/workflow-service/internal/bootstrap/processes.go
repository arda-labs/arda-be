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

//go:embed lnm-disbursement-register-v2.bpmn
var lnmDisbursementRegister []byte

//go:embed lnm-disbursement-complete-v2.bpmn
var lnmDisbursementComplete []byte

//go:embed lnm-collection-v2.bpmn
var lnmCollection []byte

//go:embed lnm-disb-batch-register-v2.bpmn
var lnmDisbBatchRegister []byte

//go:embed lnm-disb-batch-complete-v2.bpmn
var lnmDisbBatchComplete []byte

//go:embed lnm-collection-batch-v2.bpmn
var lnmCollectionBatch []byte

//go:embed hrm-employee-registration-v2.bpmn
var hrmEmployeeRegistration []byte

//go:embed dpm-settle-v2.bpmn
var dpmSettle []byte

//go:embed rpt-submit-v2.bpmn
var rptSubmit []byte

//go:embed lnm-general-provision-v2.bpmn
var lnmGeneralProvision []byte

//go:embed lnm-specific-provision-v1.bpmn
var lnmSpecificProvision []byte

//go:embed lnm-mortgage-adjust-v2.bpmn
var lnmMortgageAdjust []byte

//go:embed dpm-additional-v1.bpmn
var dpmAdditional []byte

//go:embed dpm-product-register-v1.bpmn
var dpmProductRegister []byte

//go:embed dpm-product-edit-v1.bpmn
var dpmProductEdit []byte

//go:embed cfc-contract-v1.bpmn
var cfcContract []byte

//go:embed cfc-amendment-v1.bpmn
var cfcAmendment []byte

//go:embed cfc-movement-v1.bpmn
var cfcMovement []byte

//go:embed ibm-place-v1.bpmn
var ibmPlace []byte

//go:embed ibm-movement-v1.bpmn
var ibmMovement []byte

//go:embed dpm-rate-v1.bpmn
var dpmRate []byte

//go:embed dpm-interest-v1.bpmn
var dpmInterest []byte

//go:embed fin-single-entry-v2.bpmn
var finSingleEntry []byte

//go:embed fin-double-entry-v2.bpmn
var finDoubleEntry []byte

//go:embed fin-off-balance-v2.bpmn
var finOffBalance []byte

//go:embed fin-txn-cancel-v2.bpmn
var finTxnCancel []byte

//go:embed fin-closing-v2.bpmn
var finClosing []byte

//go:embed fin-fund-appropriation-v2.bpmn
var finFundAppropriation []byte

//go:embed fin-fund-utilization-v2.bpmn
var finFundUtilization []byte

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
			ProcessCode:  "LNM_DISB_REGISTER_V2",
			Name:         "Đăng ký giải ngân (v2)",
			ResourceName: "lnm-disbursement-register-v2.bpmn",
			Content:      lnmDisbursementRegister,
		},
		{
			ProcessCode:  "LNM_DISB_COMPLETE_V2",
			Name:         "Hoàn tất giải ngân (v2)",
			ResourceName: "lnm-disbursement-complete-v2.bpmn",
			Content:      lnmDisbursementComplete,
		},
		{
			ProcessCode:  "LNM_COLLECTION_V2",
			Name:         "Thu nợ (v2)",
			ResourceName: "lnm-collection-v2.bpmn",
			Content:      lnmCollection,
		},
		{
			// Iteration 13: batch (1 hồ sơ — N hợp đồng) disbursement
			// register/complete + collection — same maker/checker skeleton as
			// the per-row flows, the workers post one N-line entry per batch.
			ProcessCode:  "LNM_DISB_BATCH_REGISTER_V2",
			Name:         "Đăng ký giải ngân theo hồ sơ (v2)",
			ResourceName: "lnm-disb-batch-register-v2.bpmn",
			Content:      lnmDisbBatchRegister,
		},
		{
			ProcessCode:  "LNM_DISB_BATCH_COMPLETE_V2",
			Name:         "Hoàn tất giải ngân theo hồ sơ (v2)",
			ResourceName: "lnm-disb-batch-complete-v2.bpmn",
			Content:      lnmDisbBatchComplete,
		},
		{
			ProcessCode:  "LNM_COLLECTION_BATCH_V2",
			Name:         "Thu nợ theo hồ sơ (v2)",
			ResourceName: "lnm-collection-batch-v2.bpmn",
			Content:      lnmCollectionBatch,
		},
		{
			ProcessCode:  "RPT_SUBMIT_V2",
			Name:         "Nộp báo cáo (v2)",
			ResourceName: "rpt-submit-v2.bpmn",
			Content:      rptSubmit,
		},
		{
			// LNM.307.01 general provision (per org, cumulative) — maker
			// screen previews the figures, checker approval posts them.
			ProcessCode:  "LNM_GENERAL_PROVISION_V2",
			Name:         "Trích lập dự phòng chung (v2)",
			ResourceName: "lnm-general-provision-v2.bpmn",
			Content:      lnmGeneralProvision,
		},
		{
			// LNM.306 specific provision: per-loan with collateral deduction;
			// provisional rule card LNM_PROVISION_306 (chờ dev_fac).
			ProcessCode:  "LNM_SPECIFIC_PROVISION_V1",
			Name:         "Trích lập dự phòng cụ thể (v1)",
			ResourceName: "lnm-specific-provision-v1.bpmn",
			Content:      lnmSpecificProvision,
		},
		{
			// Mortgage-adjust was registered as the 11th adjustment kind with
			// its own table/FE screen but never got a process — this closes
			// the kind (topics lnm.mortgage-adjust.* already registered).
			ProcessCode:  "LNM_MORTGAGE_ADJUST_V2",
			Name:         "Điều chỉnh TSBĐ (v2)",
			ResourceName: "lnm-mortgage-adjust-v2.bpmn",
			Content:      lnmMortgageAdjust,
		},
		{
			// DPM.301 additional deposit: maker submits, checker approval
			// posts the DPM_OPEN shape and bumps the savings principal.
			ProcessCode:  "DPM_ADDITIONAL_V1",
			Name:         "Nộp thêm tiền gửi (v1)",
			ResourceName: "dpm-additional-v1.bpmn",
			Content:      dpmAdditional,
		},
		{
			// DPM.102/103 product register/edit: the staged request payload is
			// applied to dpm_products only on checker approval.
			ProcessCode:  "DPM_PRODUCT_REGISTER_V1",
			Name:         "Đăng ký sản phẩm tiền gửi (v1)",
			ResourceName: "dpm-product-register-v1.bpmn",
			Content:      dpmProductRegister,
		},
		{
			ProcessCode:  "DPM_PRODUCT_EDIT_V1",
			Name:         "Điều chỉnh sản phẩm tiền gửi (v1)",
			ResourceName: "dpm-product-edit-v1.bpmn",
			Content:      dpmProductEdit,
		},
		{
			ProcessCode:  "DPM_SETTLE_V2",
			Name:         "Tất toán sổ tiết kiệm (v2)",
			ResourceName: "dpm-settle-v2.bpmn",
			Content:      dpmSettle,
		},
		{
			// CFM (vốn nội bộ): formation + amendments + movements all await
			// checker approval; posting rides CFC_* provisional cards.
			ProcessCode:  "CFC_CONTRACT_V1",
			Name:         "Hình thành hợp đồng vốn (v1)",
			ResourceName: "cfc-contract-v1.bpmn",
			Content:      cfcContract,
		},
		{
			ProcessCode:  "CFC_AMENDMENT_V1",
			Name:         "Điều chỉnh hợp đồng vốn (v1)",
			ResourceName: "cfc-amendment-v1.bpmn",
			Content:      cfcAmendment,
		},
		{
			ProcessCode:  "CFC_MOVEMENT_V1",
			Name:         "Giao dịch vốn (v1)",
			ResourceName: "cfc-movement-v1.bpmn",
			Content:      cfcMovement,
		},
		{
			// IBM (tiền gửi liên ngân hàng): placement + 4 movement kinds share
			// the movement BPMN; the kind arrives via case variables.
			ProcessCode:  "IBM_PLACE_V1",
			Name:         "Mở hợp đồng tiền gửi liên ngân hàng (v1)",
			ResourceName: "ibm-place-v1.bpmn",
			Content:      ibmPlace,
		},
		{
			ProcessCode:  "IBM_MOVEMENT_V1",
			Name:         "Giao dịch tiền gửi liên ngân hàng (v1)",
			ResourceName: "ibm-movement-v1.bpmn",
			Content:      ibmMovement,
		},
		{
			// DPM rate register/edit (DPM.100/101) shares one BPMN; the request
			// type arrives via case variables.
			ProcessCode:  "DPM_RATE_V1",
			Name:         "Đăng ký lãi suất huy động (v1)",
			ResourceName: "dpm-rate-v1.bpmn",
			Content:      dpmRate,
		},
		{
			// DPM.302/303/304 interest ops share one BPMN; single ops carry
			// opId, the batch carries opIds.
			ProcessCode:  "DPM_INTEREST_V1",
			Name:         "Trả lãi tiền gửi (v1)",
			ResourceName: "dpm-interest-v1.bpmn",
			Content:      dpmInterest,
		},
		{
			// Manual posting flows (FAC-native bút toán lẻ / bút toán kép):
			// the maker submits accountant-picked lines, the finance posting
			// rides Reserve → Validate → Post (Release on reject).
			ProcessCode:  "FIN_SINGLE_ENTRY_V2",
			Name:         "Bút toán lẻ (v2)",
			ResourceName: "fin-single-entry-v2.bpmn",
			Content:      finSingleEntry,
		},
		{
			ProcessCode:  "FIN_DOUBLE_ENTRY_V2",
			Name:         "Bút toán kép (v2)",
			ResourceName: "fin-double-entry-v2.bpmn",
			Content:      finDoubleEntry,
		},
		{
			// Iteration 10: off-balance memo posting (nhập xuất ngoại bảng)
			// — same worker shape as the manual posting legs, the lines sit
			// on nature-B accounts (availability-exempt).
			ProcessCode:  "FIN_OFF_BALANCE_V2",
			Name:         "Ngoại bảng (v2)",
			ResourceName: "fin-off-balance-v2.bpmn",
			Content:      finOffBalance,
		},
		{
			// Iteration 10: transaction cancellation — no posting request,
			// the workers reverse the referenced POSTED entry on approval.
			ProcessCode:  "FIN_TXN_CANCEL_V2",
			Name:         "Hủy giao dịch (v2)",
			ResourceName: "fin-txn-cancel-v2.bpmn",
			Content:      finTxnCancel,
		},
		{
			// Iteration 11: closing (kết chuyển thu chi FAC.203.01) — same
			// worker shape as the manual posting legs; the case variables
			// carry the server-built postingRequest (INC/EXP rows netted
			// into the FIN_CLOSING_*_DEST result account by finance-service).
			ProcessCode:  "FIN_CLOSING_V2",
			Name:         "Kết chuyển thu chi (v2)",
			ResourceName: "fin-closing-v2.bpmn",
			Content:      finClosing,
		},
		{
			// Quỹ: trích lập quỹ (Nợ 4211 / Có quỹ) — maker-checker, finance
			// builds the lines from the FUND_* class maps. Same worker shape
			// as the manual posting legs.
			ProcessCode:  "FIN_FUND_APPROP_V2",
			Name:         "Trích lập quỹ (v2)",
			ResourceName: "fin-fund-appropriation-v2.bpmn",
			Content:      finFundAppropriation,
		},
		{
			// Quỹ: sử dụng quỹ (Nợ quỹ / Có 1131).
			ProcessCode:  "FIN_FUND_USE_V2",
			Name:         "Sử dụng quỹ (v2)",
			ResourceName: "fin-fund-utilization-v2.bpmn",
			Content:      finFundUtilization,
		},
		{
			ProcessCode:  "HRM_EMPLOYEE_REGISTRATION",
			Name:         "Đăng ký nhân sự (v2)",
			ResourceName: "hrm-employee-registration-v2.bpmn",
			Content:      hrmEmployeeRegistration,
		},
		{
			// Multi-level approval flow derived from EPAS LNM.201.01
			// (hình thành khoản vay): maker → TW/PGD review → GD/Board
			// approval, wired to loan-service by the formation workers
			// (validate/execute/cancel) since iteration 11 wave 2.
			ProcessCode:  "LOAN_FORMATION_V2",
			Name:         "Hình thành khoản vay đa cấp (mẫu LNM.201.01)",
			ResourceName: "lnm-loan-formation-v2.bpmn",
			Content:      lnmLoanFormationV2,
		},
	}
}
