package repository

import "testing"

func TestValidateSLAPolicy(t *testing.T) {
	err := validateSLAPolicy(SLAPolicy{
		Code:           "SLA_FIN_IN_8H",
		Name:           "Incoming transaction same day",
		CaseType:       "FINANCE_INCOMING_TRANSACTION",
		DueInHours:     8,
		WarningInHours: 2,
		EscalationRole: "FINANCE_OPS_SUPERVISOR",
	})
	if err != nil {
		t.Fatalf("validateSLAPolicy returned error: %v", err)
	}

	err = validateSLAPolicy(SLAPolicy{
		Code:           "SLA_BAD",
		Name:           "Bad SLA",
		CaseType:       "FINANCE_INCOMING_TRANSACTION",
		DueInHours:     2,
		WarningInHours: 2,
		EscalationRole: "FINANCE_OPS_SUPERVISOR",
	})
	if err == nil {
		t.Fatal("validateSLAPolicy accepted warningInHours >= dueInHours")
	}

	err = validateSLAPolicy(SLAPolicy{
		Code:           "SLA_FIN_IN_TASKS",
		Name:           "Incoming transaction task SLA",
		CaseType:       "FINANCE_INCOMING_TRANSACTION",
		DueInHours:     8,
		WarningInHours: 2,
		EscalationRole: "FINANCE_OPS_SUPERVISOR",
		TaskPolicies: []SLATaskPolicy{{
			StepCode:       "classify-account",
			TaskName:       "Classify account",
			DurationValue:  30,
			DurationUnit:   "MINUTE",
			WarningMode:    "PERCENT",
			WarningValue:   80,
			WarningUnit:    "PERCENT",
			EscalationRole: "FINANCE_OPS_SUPERVISOR",
		}},
	})
	if err != nil {
		t.Fatalf("validateSLAPolicy with task policy returned error: %v", err)
	}
}

func TestValidateWorkflowAdminConfig(t *testing.T) {
	if err := validateDescriptionTemplate(DescriptionTemplate{
		Code:              "DESC_FIN_IN",
		BusinessSubsystem: "FAC",
		CaseType:          "FINANCE_INCOMING_TRANSACTION",
		Pattern:           "{caseCode} - {amount}",
	}); err != nil {
		t.Fatalf("validateDescriptionTemplate returned error: %v", err)
	}

	if err := validateProcessRole(ProcessRole{
		CaseType:     "FINANCE_INCOMING_TRANSACTION",
		StepCode:     "approve-journal",
		BusinessRole: "Journal checker",
		IAMRole:      "FINANCE_TXN_CHECKER",
		ActionScope:  "approve,reject,suspend",
	}); err != nil {
		t.Fatalf("validateProcessRole returned error: %v", err)
	}
}
