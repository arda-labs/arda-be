package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/arda-labs/arda/apps/hrm-service/internal/domain"
	"github.com/arda-labs/arda/apps/hrm-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

type HRMHandler struct {
	repo           *repository.HRMRepository
	workflowClient *workflowclient.Client
}

func NewHRMHandler(repo *repository.HRMRepository, workflowClient *workflowclient.Client) *HRMHandler {
	return &HRMHandler{repo: repo, workflowClient: workflowClient}
}

func (h *HRMHandler) ListPositions(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListPositions(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	writeListAll(w, r, items)
}

func (h *HRMHandler) CreatePosition(w http.ResponseWriter, r *http.Request) {
	var req domain.Position
	if !decode(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeRequired, "code and name are required")
		return
	}
	item, err := h.repo.CreatePosition(r.Context(), req)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) UpdatePosition(w http.ResponseWriter, r *http.Request) {
	var req domain.Position
	if !decode(w, r, &req) {
		return
	}
	req.ID = r.PathValue("id")
	if req.ID == "" || req.Code == "" || req.Name == "" {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeRequired, "id, code and name are required")
		return
	}
	item, err := h.repo.UpdatePosition(r.Context(), req)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) DeletePosition(w http.ResponseWriter, r *http.Request) {
	writeResult(w, r, map[string]bool{"ok": true}, h.repo.DeletePosition(r.Context(), r.PathValue("id")))
}

func (h *HRMHandler) ListJobTitles(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListJobTitles(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	writeListAll(w, r, items)
}

func (h *HRMHandler) CreateJobTitle(w http.ResponseWriter, r *http.Request) {
	var req domain.JobTitle
	if !decode(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeRequired, "code and name are required")
		return
	}
	item, err := h.repo.CreateJobTitle(r.Context(), req)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) UpdateJobTitle(w http.ResponseWriter, r *http.Request) {
	var req domain.JobTitle
	if !decode(w, r, &req) {
		return
	}
	req.ID = r.PathValue("id")
	if req.ID == "" || req.Code == "" || req.Name == "" {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeRequired, "id, code and name are required")
		return
	}
	item, err := h.repo.UpdateJobTitle(r.Context(), req)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) DeleteJobTitle(w http.ResponseWriter, r *http.Request) {
	writeResult(w, r, map[string]bool{"ok": true}, h.repo.DeleteJobTitle(r.Context(), r.PathValue("id")))
}

func (h *HRMHandler) ListOrgUnits(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListOrgUnits(r.Context(), r.URL.Query().Get("organization_id"), r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	writeListAll(w, r, items)
}

func (h *HRMHandler) CreateOrgUnit(w http.ResponseWriter, r *http.Request) {
	var req domain.OrgUnit
	if !decode(w, r, &req) {
		return
	}
	if req.Code == "" || req.OrganizationID == "" || req.Name == "" || req.OrgLevel == "" || req.DepartmentType == "" {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeRequired, "code, organization_id, name, org_level and department_type are required")
		return
	}
	item, err := h.repo.CreateOrgUnit(r.Context(), req)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) UpdateOrgUnit(w http.ResponseWriter, r *http.Request) {
	var req domain.OrgUnit
	if !decode(w, r, &req) {
		return
	}
	req.ID = r.PathValue("id")
	if req.ID == "" || req.Code == "" || req.OrganizationID == "" || req.Name == "" || req.OrgLevel == "" || req.DepartmentType == "" {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeRequired, "id, code, organization_id, name, org_level and department_type are required")
		return
	}
	item, err := h.repo.UpdateOrgUnit(r.Context(), req)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) DeleteOrgUnit(w http.ResponseWriter, r *http.Request) {
	writeResult(w, r, map[string]bool{"ok": true}, h.repo.DeleteOrgUnit(r.Context(), r.PathValue("id")))
}

func (h *HRMHandler) ListEmployees(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListEmployees(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	writeListAll(w, r, items)
}

func (h *HRMHandler) ListEmployeeRegistrations(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListEmployeeRegistrations(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	writeListAll(w, r, items)
}

func (h *HRMHandler) CreateEmployeeRegistration(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RegistrationCode string          `json:"registration_code"`
		Payload          json.RawMessage `json:"payload"`
	}
	if !decode(w, r, &req) {
		return
	}
	payload := string(req.Payload)
	if payload == "" {
		payload = "{}"
	}
	item, err := h.repo.CreateEmployeeRegistration(r.Context(), domain.EmployeeRegistration{
		RegistrationCode: req.RegistrationCode,
		Payload:          payload,
		CreatedBy:        headerPtr(r, "X-User-Id"),
	})
	writeResult(w, r, item, err)
}

func (h *HRMHandler) UpdateEmployeeRegistration(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Payload json.RawMessage `json:"payload"`
	}
	if !decode(w, r, &req) {
		return
	}
	payload := string(req.Payload)
	if payload == "" {
		payload = "{}"
	}
	item, err := h.repo.UpdateEmployeeRegistration(r.Context(), r.PathValue("id"), payload)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) SubmitEmployeeRegistration(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkflowCaseID string `json:"workflow_case_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	existing, err := h.repo.GetEmployeeRegistration(r.Context(), r.PathValue("id"))
	if err != nil {
		writeResult(w, r, existing, err)
		return
	}
	if req.WorkflowCaseID == "" && existing.WorkflowCaseID != nil {
		req.WorkflowCaseID = *existing.WorkflowCaseID
	}
	if req.WorkflowCaseID == "" {
		caseID, err := h.submitWorkflowCase(r, r.PathValue("id"))
		if err != nil {
			writeErrorCode(w, r, http.StatusBadGateway, ardaerrors.CodeBadGateway, "failed to submit workflow case: "+err.Error())
			return
		}
		req.WorkflowCaseID = caseID
	}
	item, err := h.repo.SubmitEmployeeRegistration(r.Context(), r.PathValue("id"), req.WorkflowCaseID)
	writeResult(w, r, item, err)
}

func (h *HRMHandler) submitWorkflowCase(r *http.Request, registrationID string) (string, error) {
	if h.workflowClient == nil {
		return "", fmt.Errorf("workflow client is not configured")
	}
	actor := r.Header.Get("X-User-Id")
	if actor == "" {
		actor = "hrm-submitter"
	}
	createdCase, err := h.workflowClient.CreateCase(r.Context(), workflowclient.CaseCreate{
		TenantID:          "default",
		CaseType:          "HRM_EMPLOYEE_REGISTRATION",
		Title:             "Dang ky nhan su " + registrationID,
		PrimaryObjectType: "HRM_EMPLOYEE_REGISTRATION",
		PrimaryObjectID:   registrationID,
		DomainService:     "hrm-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
	})
	if err != nil {
		return "", err
	}
	if createdCase.GetId() == "" {
		return "", fmt.Errorf("workflow case id is empty")
	}
	_, err = h.workflowClient.SubmitCase(r.Context(), createdCase.GetId(), actor, map[string]any{
		"employeeRegistrationId": registrationID,
	})
	if err != nil {
		return "", err
	}
	return createdCase.GetId(), nil
}

func headerPtr(r *http.Request, key string) *string {
	v := r.Header.Get(key)
	if v == "" {
		return nil
	}
	return &v
}
