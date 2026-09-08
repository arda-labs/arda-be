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
	// Deposit settlement (dpm-settle-v2.bpmn)
	"dpm.settle.validate",
	"dpm.settle.execute",
	"dpm.settle.cancel",
	// Collection (lnm-collection-v2.bpmn)
	"lnm.collection.validate",
	"lnm.collection.execute",
	"lnm.collection.cancel",
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
