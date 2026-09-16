package grpc

import "testing"

func TestEmployeeFromRegistration(t *testing.T) {
	payload := `{
		"employee_code": " NV-001 ",
		"full_name": " Nguyen Van A ",
		"org_unit_id": "ou-1",
		"assignments": [
			{"position_id": ""},
			{"position_id": "pos-9"}
		]
	}`
	employee, err := employeeFromRegistration("DKNV-T-20260916-000001", payload)
	if err != nil {
		t.Fatalf("employeeFromRegistration: %v", err)
	}
	if employee.EmployeeCode != "NV-001" {
		t.Fatalf("EmployeeCode = %q, want NV-001", employee.EmployeeCode)
	}
	if employee.FullName != "Nguyen Van A" {
		t.Fatalf("FullName = %q, want Nguyen Van A", employee.FullName)
	}
	if employee.OrgUnitID == nil || *employee.OrgUnitID != "ou-1" {
		t.Fatalf("OrgUnitID = %v, want ou-1", employee.OrgUnitID)
	}
	if employee.PositionID == nil || *employee.PositionID != "pos-9" {
		t.Fatalf("PositionID = %v, want pos-9 (first non-empty assignment)", employee.PositionID)
	}
}

func TestEmployeeFromRegistrationFallsBackToRegistrationCode(t *testing.T) {
	payload := `{
		"full_name": "Tran Thi B",
		"org_unit_id": "ou-2",
		"assignments": []
	}`
	employee, err := employeeFromRegistration("DKNV-T-20260916-000002", payload)
	if err != nil {
		t.Fatalf("employeeFromRegistration: %v", err)
	}
	if employee.EmployeeCode != "DKNV-T-20260916-000002" {
		t.Fatalf("EmployeeCode = %q, want the registration code fallback", employee.EmployeeCode)
	}
	if employee.PositionID != nil {
		t.Fatalf("PositionID = %v, want nil without assignments", employee.PositionID)
	}
}

func TestEmployeeFromRegistrationRejectsMissingRequiredFields(t *testing.T) {
	if _, err := employeeFromRegistration("DKNV-T-1", `{"org_unit_id":"ou-1"}`); err == nil {
		t.Fatal("expected missing full_name to fail")
	}
	if _, err := employeeFromRegistration("DKNV-T-1", "not-json"); err == nil {
		t.Fatal("expected invalid payload json to fail")
	}
}

func TestRegistrationStatusHelpers(t *testing.T) {
	for _, status := range []string{"SUBMITTED", "submitted", " IN_REVIEW "} {
		if !isActionableRegistrationStatus(status) {
			t.Fatalf("isActionableRegistrationStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"", "DRAFT", "APPROVED", "REJECTED"} {
		if isActionableRegistrationStatus(status) {
			t.Fatalf("isActionableRegistrationStatus(%q) = true, want false", status)
		}
	}
	for _, status := range []string{"submitted", "IN_REVIEW", " approved "} {
		if !isSettleableRegistrationStatus(status) {
			t.Fatalf("isSettleableRegistrationStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"", "draft", "REJECTED"} {
		if isSettleableRegistrationStatus(status) {
			t.Fatalf("isSettleableRegistrationStatus(%q) = true, want false", status)
		}
	}
}
