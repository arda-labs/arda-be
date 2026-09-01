package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/hrm-service/internal/domain"
)

func TestToAIEmployees_RedactsInternalFields(t *testing.T) {
	orgRef := "org-1"
	userRef := "iam-1"
	items := []domain.Employee{{
		ID: "e1", TenantID: "tenant-1", EmployeeCode: "NV-001", FullName: "Nguyen Van A",
		OrgUnitID: &orgRef, PositionID: &orgRef, JobTitleID: &orgRef, IAMUserID: &userRef,
		Status: "ACTIVE", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}

	raw, err := json.Marshal(toAIEmployees(items))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	allowed := map[string]bool{"id": true, "employeeCode": true, "fullName": true, "status": true}
	for key := range out[0] {
		if !allowed[key] {
			t.Errorf("field %q leaked into the AI shape (allowlist violation)", key)
		}
	}
	if out[0]["employeeCode"] != "NV-001" || out[0]["fullName"] != "Nguyen Van A" {
		t.Errorf("unexpected redacted payload: %v", out[0])
	}
}

func TestInternalAIListEmployees_MethodAndFilter(t *testing.T) {
	h := &HRMHandler{} // repo is nil: method check must fire before any repo call

	rec := httptest.NewRecorder()
	h.InternalAIListEmployees(rec, httptest.NewRequest(http.MethodPost, "/internal/ai/employees", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}
}
