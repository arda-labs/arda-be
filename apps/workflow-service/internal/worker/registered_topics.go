package worker

// RegisteredJobTopics enumerates every Zeebe job topic a live worker
// subscribes to in cmd/workflow-service/main.go. The bootstrap coverage test
// cross-checks it against the serviceTask topics declared in the embedded
// BPMN files, so a process can never reference a topic without a handler —
// the failure mode where a case hangs forever at its terminal APPROVE/REJECT
// step. Keep this list in sync when adding or removing workers in main.go.
var RegisteredJobTopics = []string{
	// CRM adjustment (customer-adjustment-v2.bpmn + legacy v1 flows)
	"crm.request_customer_changes",
	"crm.reject_customer",
	"crm.approve_customer",
	"crm.update_customer",
	// CRM registration (crm-customer-registration-v2.bpmn)
	"crm.customer.register.validate",
	"crm.customer.register.execute",
	"crm.customer.register.cancel",
	// Disbursement two-flow (lnm-disbursement-register-v2.bpmn +
	// lnm-disbursement-complete-v2.bpmn)
	"lnm.disb-register.init",
	"lnm.disb-register.validate",
	"lnm.disb-register.execute",
	"lnm.disb-register.cancel",
	"lnm.disb-complete.init",
	"lnm.disb-complete.validate",
	"lnm.disb-complete.execute",
	"lnm.disb-complete.cancel",
	// HRM employee registration (hrm-employee-registration-v2.bpmn)
	"hrm.employee.register.validate",
	"hrm.employee.register.execute",
	"hrm.employee.register.cancel",
	// Report submission (rpt-submit-v2.bpmn)
	"rpt.submit.validate",
	"rpt.submit.execute",
	"rpt.submit.cancel",
	// General provision (lnm-general-provision-v2.bpmn)
	"lnm.general-provision.validate",
	"lnm.general-provision.execute",
	"lnm.general-provision.cancel",
	// Deposit settlement (dpm-settle-v2.bpmn)
	"dpm.settle.validate",
	"dpm.settle.execute",
	"dpm.settle.cancel",
	// Additional deposit (dpm-additional-v1.bpmn)
	"dpm.additional.validate",
	"dpm.additional.execute",
	"dpm.additional.cancel",
	// Product register/edit (dpm-product-register-v1 + dpm-product-edit-v1)
	"dpm.product-register.validate",
	"dpm.product-register.execute",
	"dpm.product-register.cancel",
	"dpm.product-edit.validate",
	"dpm.product-edit.execute",
	"dpm.product-edit.cancel",
	// CFM lifecycle (cfc-contract-v1 + cfc-amendment-v1 + cfc-movement-v1)
	"cfc.contract.validate",
	"cfc.contract.execute",
	"cfc.contract.cancel",
	"cfc.amendment.validate",
	"cfc.amendment.execute",
	"cfc.amendment.cancel",
	"cfc.movement.validate",
	"cfc.movement.execute",
	"cfc.movement.cancel",
	// IBM lifecycle (ibm-place-v1 + ibm-movement-v1)
	"ibm.place.validate",
	"ibm.place.execute",
	"ibm.place.cancel",
	"ibm.movement.validate",
	"ibm.movement.execute",
	"ibm.movement.cancel",
	// DPM rates + interest ops (dpm-rate-v1 + dpm-interest-v1)
	"dpm.rate.validate",
	"dpm.rate.execute",
	"dpm.rate.cancel",
	"dpm.interest.validate",
	"dpm.interest.execute",
	"dpm.interest.cancel",
	// Loan formation (lnm-loan-formation-v2.bpmn, iteration 11 wave 2) —
	// multi-level approval; UT_* steps are human workbench tasks.
	"lnm.loan.formation.validate",
	"lnm.loan.formation.execute",
	"lnm.loan.formation.cancel",
	// Collection (lnm-collection-v2.bpmn) — two-phase reserve mirror of the
	// disbursement register leg.
	"lnm.collection.init",
	"lnm.collection.validate",
	"lnm.collection.execute",
	"lnm.collection.cancel",
	// Batch flows (iteration 13 — 1 hồ sơ — N hợp đồng):
	// lnm-disb-batch-register-v2.bpmn + lnm-disb-batch-complete-v2.bpmn +
	// lnm-collection-batch-v2.bpmn
	"lnm.disb-batch-register.init",
	"lnm.disb-batch-register.validate",
	"lnm.disb-batch-register.execute",
	"lnm.disb-batch-register.cancel",
	"lnm.disb-batch-complete.init",
	"lnm.disb-batch-complete.validate",
	"lnm.disb-batch-complete.execute",
	"lnm.disb-batch-complete.cancel",
	"lnm.collection-batch.init",
	"lnm.collection-batch.validate",
	"lnm.collection-batch.execute",
	"lnm.collection-batch.cancel",
	// Manual posting two-flow (fin-single-entry-v2.bpmn +
	// fin-double-entry-v2.bpmn)
	"fin.single-entry.init",
	"fin.single-entry.validate",
	"fin.single-entry.execute",
	"fin.single-entry.cancel",
	"fin.double-entry.init",
	"fin.double-entry.validate",
	"fin.double-entry.execute",
	"fin.double-entry.cancel",
	// Off-balance memo posting (fin-off-balance-v2.bpmn, iteration 10)
	"fin.off-balance.init",
	"fin.off-balance.validate",
	"fin.off-balance.execute",
	"fin.off-balance.cancel",
	// Transaction cancellation (fin-txn-cancel-v2.bpmn, iteration 10)
	"fin.txn-cancel.init",
	"fin.txn-cancel.validate",
	"fin.txn-cancel.execute",
	"fin.txn-cancel.cancel",
	// Closing / kết chuyển thu chi (fin-closing-v2.bpmn, iteration 11)
	"fin.closing.init",
	"fin.closing.validate",
	"fin.closing.execute",
	"fin.closing.cancel",
	// Notification (no BPMN service task yet; registered for future flows)
	"notification.email",
	"notification.sms",
	"notification.push",
	"notification.customer_registration_result",
}

// RegisteredLNMTopics returns the loan adjustment topics for one flow kind;
// workers register validate/execute/cancel per kind in main.go.
func RegisteredLNMTopics(kinds []string) []string {
	topics := make([]string, 0, len(kinds)*3)
	for _, kind := range kinds {
		topics = append(topics, "lnm."+kind+".validate", "lnm."+kind+".execute", "lnm."+kind+".cancel")
	}
	return topics
}
