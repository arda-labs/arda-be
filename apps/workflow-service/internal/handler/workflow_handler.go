package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/notificationclient"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	crmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/crm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WorkflowHandler struct {
	zeebeSvc           *service.ZeebeService
	zeebeRest          *service.ZeebeRestClient
	crmClient          *crmclient.Client
	notificationClient *notificationclient.Client
	workflowCmd        *service.WorkflowCommandService
	mappingRepo        *repository.MappingRepository
	caseRepo           *repository.CaseRepository
	processDefinition  *repository.ProcessDefinitionRepository
}

func NewWorkflowHandler(
	zeebeSvc *service.ZeebeService,
	zeebeRest *service.ZeebeRestClient,
	crmClient *crmclient.Client,
	mappingRepo *repository.MappingRepository,
	caseRepo *repository.CaseRepository,
	processDefinition *repository.ProcessDefinitionRepository,
) *WorkflowHandler {
	return &WorkflowHandler{
		zeebeSvc:          zeebeSvc,
		zeebeRest:         zeebeRest,
		crmClient:         crmClient,
		workflowCmd:       service.NewWorkflowCommandService(caseRepo, zeebeSvc),
		mappingRepo:       mappingRepo,
		caseRepo:          caseRepo,
		processDefinition: processDefinition,
	}
}

func (h *WorkflowHandler) SetNotificationClient(client *notificationclient.Client) {
	h.notificationClient = client
}

func (h *WorkflowHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	err := r.ParseMultipartForm(10 << 20) // 10MB
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Failed to parse form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Missing file: "+err.Error())
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to read file: "+err.Error())
		return
	}

	key, err := h.zeebeSvc.DeployWorkflow(r.Context(), header.Filename, content)
	if err != nil {
		writeDeployError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"processDefinitionKey": key,
		"filename":             header.Filename,
		"status":               "deployed",
	})
}

type StartRequest struct {
	BpmnProcessID string         `json:"bpmnProcessId"`
	BusinessKey   string         `json:"businessKey"`
	Variables     map[string]any `json:"variables"`
}

func (h *WorkflowHandler) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.BpmnProcessID == "" {
		writeAPIError(w, r, http.StatusBadRequest, "bpmnProcessId is required")
		return
	}

	// Include businessKey in variables automatically so workers can easily access it
	if req.Variables == nil {
		req.Variables = make(map[string]any)
	}
	if req.BusinessKey != "" {
		req.Variables["businessKey"] = req.BusinessKey
	}

	key, err := h.zeebeSvc.StartWorkflow(r.Context(), req.BpmnProcessID, req.Variables)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to start workflow: "+err.Error())
		return
	}

	if req.BusinessKey != "" {
		err = h.mappingRepo.SaveMapping(r.Context(), req.BusinessKey, key, req.BpmnProcessID, "ACTIVE")
		if err != nil {
			// Log error but proceed
			writeAPIError(w, r, http.StatusInternalServerError, "Workflow started but failed to save mapping: "+err.Error())
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"processInstanceKey": key,
		"businessKey":        req.BusinessKey,
		"status":             "started",
	})
}

type MessageRequest struct {
	MessageName    string         `json:"messageName"`
	CorrelationKey string         `json:"correlationKey"`
	MessageID      string         `json:"messageId"`
	Variables      map[string]any `json:"variables"`
}

func (h *WorkflowHandler) PublishMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.MessageName == "" || req.CorrelationKey == "" {
		writeAPIError(w, r, http.StatusBadRequest, "messageName and correlationKey are required")
		return
	}

	_, err := h.zeebeSvc.PublishMessage(r.Context(), req.MessageName, req.CorrelationKey, req.MessageID, req.Variables)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to publish message: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "published",
	})
}

func (h *WorkflowHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 2 {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid path")
		return
	}

	instanceKeyStr := parts[len(parts)-2]
	instanceKey, err := strconv.ParseInt(instanceKeyStr, 10, 64)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid instance key: "+instanceKeyStr)
		return
	}

	err = h.zeebeSvc.CancelWorkflow(r.Context(), instanceKey)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to cancel workflow: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"processInstanceKey": instanceKey,
		"status":             "cancelled",
	})
}

func (h *WorkflowHandler) GetMapping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 2 {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid path")
		return
	}

	businessKey := parts[len(parts)-1]
	mapping, err := h.mappingRepo.GetMapping(r.Context(), businessKey)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query mapping: "+err.Error())
		return
	}

	if mapping == nil {
		writeAPIError(w, r, http.StatusNotFound, "Mapping not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mapping)
}

func (h *WorkflowHandler) CaseTypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		caseTypes, err := h.caseRepo.ListCaseTypes(r.Context())
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to query case types: "+err.Error())
			return
		}
		writeJSON(w, r, http.StatusOK, caseTypes)
	case http.MethodPost:
		var req repository.CaseTypeUpsert
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateCaseType(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) CaseTypeByID(w http.ResponseWriter, r *http.Request) {
	caseType, action := caseTypePath(r.URL.Path)
	if caseType == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut || (action != "" && action != "process-config") {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if action == "process-config" {
		var req repository.ProcessConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.UpdateProcessConfig(r.Context(), caseType, req)
		if err != nil {
			writeAPIError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		if item == nil {
			writeAPIError(w, r, http.StatusNotFound, "Case type not found")
			return
		}
		writeJSON(w, r, http.StatusOK, item)
		return
	}

	var req repository.CaseTypeUpsert
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateCaseType(r.Context(), caseType, req)
	writeUpdateOrError(w, r, item, err, "Case type not found")
}

func (h *WorkflowHandler) SLAPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListSLAPolicies(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.SLAPolicy
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateSLAPolicy(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) SLAPolicyByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/sla-policies/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req repository.SLAPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateSLAPolicy(r.Context(), id, req)
	writeUpdateOrError(w, r, item, err, "SLA policy not found")
}

func (h *WorkflowHandler) DescriptionTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListDescriptionTemplates(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.DescriptionTemplate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateDescriptionTemplate(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) DescriptionTemplateByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/description-templates/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req repository.DescriptionTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateDescriptionTemplate(r.Context(), id, req)
	writeUpdateOrError(w, r, item, err, "Description template not found")
}

func (h *WorkflowHandler) ProcessRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListProcessRoles(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.ProcessRole
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateProcessRole(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) ProcessDefinitions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.processDefinition.List(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		in, err := parseProcessDefinitionImport(r)
		if err != nil {
			writeAPIError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		item, err := h.processDefinition.Create(r.Context(), in)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) ProcessDefinitionByID(w http.ResponseWriter, r *http.Request) {
	id, action := processDefinitionPath(r.URL.Path)
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodPut && action == "":
		in, err := parseProcessDefinitionImport(r)
		if err != nil {
			writeAPIError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		item, err := h.processDefinition.Update(r.Context(), id, in)
		writeUpdateOrError(w, r, item, err, "Process definition not found")
	case r.Method == http.MethodDelete && action == "":
		deleted, err := h.processDefinition.Delete(r.Context(), id)
		writeDeleteOrError(w, r, deleted, err, "Process definition not found")
	case r.Method == http.MethodGet && action == "xml":
		item, err := h.processDefinition.Get(r.Context(), id)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to query process definition: "+err.Error())
			return
		}
		if item == nil {
			writeAPIError(w, r, http.StatusNotFound, "Process definition not found")
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(item.ResourceName)+`"`)
		_, _ = w.Write([]byte(item.XMLContent))
	case r.Method == http.MethodPost && action == "deploy":
		h.deployProcessDefinition(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (h *WorkflowHandler) deployProcessDefinition(w http.ResponseWriter, r *http.Request, id string) {
	if h.zeebeSvc == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe service is not configured")
		return
	}
	item, err := h.processDefinition.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query process definition: "+err.Error())
		return
	}
	if item == nil {
		writeAPIError(w, r, http.StatusNotFound, "Process definition not found")
		return
	}

	key, err := h.zeebeSvc.DeployWorkflow(r.Context(), item.ResourceName, []byte(item.XMLContent))
	if err != nil {
		writeDeployError(w, r, err)
		return
	}
	deployed, err := h.processDefinition.MarkDeployed(r.Context(), id, key)
	writeUpdateOrError(w, r, deployed, err, "Process definition not found")
}

func (h *WorkflowHandler) ProcessRoleByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/roles/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req repository.ProcessRole
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateProcessRole(r.Context(), id, req)
	writeUpdateOrError(w, r, item, err, "Process role not found")
}

func (h *WorkflowHandler) RoleCatalog(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowRoleCatalog(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.WorkflowRoleCatalog
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateWorkflowRoleCatalog(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) RoleCatalogByCode(w http.ResponseWriter, r *http.Request) {
	roleCode := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/role-catalog/"), "/")
	if roleCode == "" || strings.Contains(roleCode, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req repository.WorkflowRoleCatalog
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateWorkflowRoleCatalog(r.Context(), roleCode, req)
	writeUpdateOrError(w, r, item, err, "Workflow role not found")
}

func (h *WorkflowHandler) RoleMemberships(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowRoleMemberships(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.WorkflowRoleMembership
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateWorkflowRoleMembership(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) RoleMembershipByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/role-memberships/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req repository.WorkflowRoleMembership
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateWorkflowRoleMembership(r.Context(), id, req)
	writeUpdateOrError(w, r, item, err, "Workflow role membership not found")
}

func (h *WorkflowHandler) AssignmentRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowAssignmentRules(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.WorkflowAssignmentRule
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateWorkflowAssignmentRule(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) AssignmentRuleByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/assignment-rules/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req repository.WorkflowAssignmentRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateWorkflowAssignmentRule(r.Context(), id, req)
	writeUpdateOrError(w, r, item, err, "Workflow assignment rule not found")
}

func (h *WorkflowHandler) Delegations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowDelegations(r.Context())
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.WorkflowDelegation
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateWorkflowDelegation(r.Context(), req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) DelegationByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/delegations/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req repository.WorkflowDelegation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	item, err := h.caseRepo.UpdateWorkflowDelegation(r.Context(), id, req)
	writeUpdateOrError(w, r, item, err, "Workflow delegation not found")
}

func (h *WorkflowHandler) HealthReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	payload := map[string]any{"status": "ready"}
	if h.zeebeSvc == nil {
		payload["zeebe"] = "not_configured"
		writeJSON(w, r, http.StatusServiceUnavailable, payload)
		return
	}
	if err := h.zeebeSvc.HealthCheck(r.Context()); err != nil {
		payload["status"] = "degraded"
		payload["zeebe"] = "unreachable"
		payload["zeebeError"] = err.Error()
		writeJSON(w, r, http.StatusServiceUnavailable, payload)
		return
	}
	payload["zeebe"] = "ok"
	writeJSON(w, r, http.StatusOK, payload)
}

func (h *WorkflowHandler) Cases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listCases(w, r)
	case http.MethodPost:
		h.createCase(w, r)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) WorkItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := h.seedWorkItems(r.Context(), r.URL.Query().Get("direction")); err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to prepare work items: "+err.Error())
		return
	}
	items, err := h.caseRepo.ListWorkItems(r.Context(), workItemFilter(r))
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work items: "+err.Error())
		return
	}
	if err := h.applyWorkItemPermissions(r.Context(), r, items); err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to resolve task permissions: "+err.Error())
		return
	}
	// Incoming only: filter to items user can claim or is assigned to.
	// Search (ALL) and outgoing must not use this gate — otherwise search
	// looks empty when the caller omitted/lost direction=ALL.
	dir := strings.ToUpper(r.URL.Query().Get("direction"))
	if dir == "" || dir == "INCOMING" {
		filtered := items[:0]
		for _, item := range items {
			if item.CanClaim || item.CanOpen {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

func (h *WorkflowHandler) WorkItemSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := h.seedWorkItems(r.Context(), r.URL.Query().Get("direction")); err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to prepare work items: "+err.Error())
		return
	}
	filter := workItemFilter(r)
	filter.Limit = 200
	items, err := h.caseRepo.ListWorkItems(r.Context(), filter)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work item summary: "+err.Error())
		return
	}
	mineFilter := filter
	mineFilter.Scope = "MINE"
	mineItems, err := h.caseRepo.ListWorkItems(r.Context(), mineFilter)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work item summary: "+err.Error())
		return
	}
	allItems := append(items, mineItems...)
	writeJSON(w, r, http.StatusOK, map[string]any{"nodes": workItemSummary(allItems, currentUserID(r))})
}

func (h *WorkflowHandler) WorkItemByID(w http.ResponseWriter, r *http.Request) {
	id, action := workItemPath(r.URL.Path)
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		item, err := h.caseRepo.GetWorkItem(r.Context(), id, currentUserID(r))
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work item: "+err.Error())
			return
		}
		if item == nil {
			writeAPIError(w, r, http.StatusNotFound, "Work item not found")
			return
		}
		writeJSON(w, r, http.StatusOK, item)
		return
	}
	if action != "claim" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		writeAPIError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	item, err := h.caseRepo.GetWorkItem(r.Context(), id, userID)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work item: "+err.Error())
		return
	}
	if item == nil {
		writeAPIError(w, r, http.StatusNotFound, "Work item not found")
		return
	}
	ok, err := h.canClaimCandidateRole(r.Context(), r, item.CandidateRole)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to resolve claim permission: "+err.Error())
		return
	}
	if !ok && item.AssignedTo != userID {
		writeAPIError(w, r, http.StatusForbidden, "User is not in candidate role/group for this task")
		return
	}
	if item.JobKey == nil && h.zeebeSvc != nil {
		filter := taskClaimFilterForWorkItem(r.Context(), h.caseRepo, item)
		if filter.ElementID == "" && strings.TrimSpace(item.StepCode) != "" {
			filter.ElementID = strings.TrimSpace(item.StepCode)
		}
		role := strings.TrimSpace(item.CandidateRole)
		if role == "" {
			role = roleForTaskType(item.TaskType)
		}
		jobType := strings.TrimSpace(item.TaskType)
		if jobType == "" {
			jobType = taskTypeForRequest(role, "")
		}
		slog.Info("work item claim resolving zeebe task",
			"workItemId", id,
			"userId", userID,
			"caseId", item.CaseID,
			"role", role,
			"taskType", item.TaskType,
			"stepCode", item.StepCode,
			"processInstanceKey", filter.ProcessInstanceKey,
		)
		if task, source, err := h.tryCachedClaimTask(r.Context(), jobType, filter); err == nil && task != nil {
			slog.Info("work item claim bound cached task",
				"workItemId", id,
				"source", source,
				"jobKey", task.JobKey,
				"processInstanceKey", task.ProcessInstanceKey,
			)
			_, _ = h.caseRepo.UpsertWorkItem(r.Context(), repository.WorkItemSeed{
				CaseID:             item.CaseID,
				ProcessInstanceKey: int64Ptr(task.ProcessInstanceKey),
				JobKey:             int64Ptr(task.JobKey),
				TaskType:           task.Type,
				StepCode:           task.ElementID,
				CandidateRole:      firstString(task.CandidateRole, item.CandidateRole),
				SLADueAt:           item.SLADueAt,
				Title:              item.Title,
				Description:        item.Description,
			})
		} else {
			claimCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			var task *service.WorkflowTask
			var err error
			if native, nativeErr := h.tryNativeUserTaskClaim(claimCtx, filter, filter.ElementID, userID); nativeErr != nil {
				slog.Warn("work item claim native user task failed",
					"workItemId", id,
					"caseId", item.CaseID,
					"err", nativeErr,
				)
			} else if native != nil {
				task = native
			}
			cancel()
			if task == nil && err == nil {
				err = fmt.Errorf("legacy parked user-task runtime has been removed; migrate this process to native BPMN userTask")
			}
			if err == nil && task != nil {
				slog.Info("work item claim bound zeebe task",
					"workItemId", id,
					"jobKey", task.JobKey,
					"processInstanceKey", task.ProcessInstanceKey,
					"elementId", task.ElementID,
					"jobType", task.Type,
				)
				_, _ = h.caseRepo.UpsertWorkItem(r.Context(), repository.WorkItemSeed{
					CaseID:             item.CaseID,
					ProcessInstanceKey: int64Ptr(task.ProcessInstanceKey),
					JobKey:             int64Ptr(task.JobKey),
					TaskType:           task.Type,
					StepCode:           task.ElementID,
					CandidateRole:      firstString(task.CandidateRole, item.CandidateRole),
					SLADueAt:           item.SLADueAt,
					Title:              item.Title,
					Description:        item.Description,
				})
				_ = h.caseRepo.MarkCaseAtStep(r.Context(), task.ProcessInstanceKey, task.ElementID, task.CandidateRole)
			} else {
				slog.Warn("work item claim could not bind zeebe task",
					"workItemId", id,
					"caseId", item.CaseID,
					"role", role,
					"processInstanceKey", filter.ProcessInstanceKey,
					"err", err,
					"hint", claimUnavailableMessage(r.Context(), h.caseRepo, filter, jobType, err),
				)
			}
		}
	}
	claimed, err := h.caseRepo.ClaimWorkItem(r.Context(), id, userID)
	if err != nil {
		writeAPIError(w, r, http.StatusConflict, err.Error())
		return
	}
	if claimed == nil {
		writeAPIError(w, r, http.StatusNotFound, "Work item not found")
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"workItem": claimed, "claimedBy": userID, "claimedAt": time.Now()})
}

func (h *WorkflowHandler) Tasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	role := r.URL.Query().Get("role")
	jobType := taskTypeForRequest(role, r.URL.Query().Get("task_type"))
	if jobType == "" {
		writeAPIError(w, r, http.StatusBadRequest, "Unsupported task role or task_type")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	tasks, err := h.listTaskCandidates(r.Context(), role, jobType, limit)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query task candidates: "+err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": tasks})
}

func (h *WorkflowHandler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if h.zeebeSvc == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe service is not configured")
		return
	}
	var req struct {
		Role               string    `json:"role"`
		TaskType           string    `json:"taskType"`
		ProcessInstanceKey flexInt64 `json:"processInstanceKey"`
		CaseID             string    `json:"caseId"`
		ElementID          string    `json:"elementId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	jobType := taskTypeForRequest(req.Role, req.TaskType)
	if jobType == "" && req.ProcessInstanceKey.Int64() == 0 && strings.TrimSpace(req.CaseID) == "" {
		writeAPIError(w, r, http.StatusBadRequest, "Unsupported task role or taskType")
		return
	}
	filter := service.TaskClaimFilter{
		ProcessInstanceKey: req.ProcessInstanceKey.Int64(),
		CaseID:             strings.TrimSpace(req.CaseID),
		ElementID:          strings.TrimSpace(req.ElementID),
	}
	actor := currentUserID(r)
	slog.Info("workflow task claim requested",
		"actor", actor,
		"role", req.Role,
		"taskType", req.TaskType,
		"resolvedJobType", jobType,
		"caseId", filter.CaseID,
		"elementId", filter.ElementID,
		"processInstanceKey", filter.ProcessInstanceKey,
	)

	if task, source, err := h.tryCachedClaimTask(r.Context(), jobType, filter); err != nil {
		slog.Error("workflow task claim failed resolving cached work task",
			"caseId", filter.CaseID,
			"processInstanceKey", filter.ProcessInstanceKey,
			"err", err,
		)
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to resolve work task: "+err.Error())
		return
	} else if task != nil {
		slog.Info("workflow task claim served from cache",
			"source", source,
			"caseId", task.CaseID,
			"jobKey", task.JobKey,
			"processInstanceKey", task.ProcessInstanceKey,
			"stepCode", task.ElementID,
		)
		writeJSON(w, r, http.StatusOK, task)
		return
	}

	claimCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	startedAt := time.Now()
	if task, err := h.tryNativeUserTaskClaim(claimCtx, filter, filter.ElementID, actor); err != nil {
		slog.Warn("native user task claim failed", "err", err)
		if h.usesNativeUserTaskRuntime(r.Context(), filter) {
			writeJSON(w, r, http.StatusBadGateway, map[string]any{
				"error": nativeClaimUnavailableMessage(filter, err),
			})
			return
		}
	} else if task != nil {
		h.persistInboxClaim(r.Context(), *task)
		writeJSON(w, r, http.StatusOK, task)
		return
	}

	if h.usesNativeUserTaskRuntime(r.Context(), filter) {
		writeJSON(w, r, http.StatusNotFound, map[string]any{
			"error": nativeClaimUnavailableMessage(filter, fmt.Errorf("no active native user task for element %q", filter.ElementID)),
		})
		return
	}

	err := fmt.Errorf("legacy parked user-task runtime has been removed; migrate this process to native BPMN userTask")
	slog.Warn("workflow task claim unavailable",
		"actor", actor,
		"role", req.Role,
		"processInstanceKey", filter.ProcessInstanceKey,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"err", err,
	)
	writeJSON(w, r, http.StatusGone, map[string]any{
		"error": claimUnavailableMessage(r.Context(), h.caseRepo, filter, jobType, err),
	})
}

func workItemToWorkflowTask(item repository.WorkItem) service.WorkflowTask {
	task := service.WorkflowTask{
		CaseID:        item.CaseID,
		CaseCode:      item.CaseCode,
		CustomerID:    item.PrimaryObjectID,
		CandidateRole: item.CandidateRole,
		Type:          item.TaskType,
		ElementID:     item.StepCode,
	}
	if item.JobKey != nil {
		task.JobKey = *item.JobKey
	}
	if item.ProcessInstanceKey != nil {
		task.ProcessInstanceKey = *item.ProcessInstanceKey
	}
	return task
}

func (h *WorkflowHandler) persistInboxClaim(ctx context.Context, task service.WorkflowTask) {
	if strings.TrimSpace(task.CaseID) == "" {
		return
	}
	_, _ = h.caseRepo.UpsertWorkItem(ctx, repository.WorkItemSeed{
		CaseID:             task.CaseID,
		ProcessInstanceKey: int64Ptr(task.ProcessInstanceKey),
		JobKey:             int64Ptr(task.JobKey),
		TaskType:           task.Type,
		StepCode:           task.ElementID,
		CandidateRole:      task.CandidateRole,
		Title:              taskLabelForType(task.Type),
	})
}

func (h *WorkflowHandler) listTaskCandidates(ctx context.Context, role string, jobType string, limit int) ([]service.WorkflowTask, error) {
	cases, err := h.caseRepo.ListCases(ctx, repository.CaseListFilter{
		CandidateRole: role,
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]service.WorkflowTask, 0, len(cases))
	for _, item := range cases {
		if item.Status != repository.CaseStatusSubmitted && item.Status != repository.CaseStatusInReview {
			continue
		}
		processInstanceKey := int64(0)
		if item.ProcessInstanceKey != nil {
			processInstanceKey = *item.ProcessInstanceKey
		}
		out = append(out, service.WorkflowTask{
			Type:               jobType,
			ElementID:          item.CurrentStep,
			ProcessInstanceKey: processInstanceKey,
			CaseID:             item.ID,
			CaseCode:           item.CaseCode,
			CustomerID:         item.PrimaryObjectID,
			CandidateRole:      role,
			SLADueAt:           item.SLADueAt,
			Variables: map[string]any{
				"caseId":            item.ID,
				"caseCode":          item.CaseCode,
				"caseType":          item.CaseType,
				"domainService":     item.DomainService,
				"primaryObjectType": item.PrimaryObjectType,
				"primaryObjectId":   item.PrimaryObjectID,
			},
		})
	}
	return out, nil
}

func (h *WorkflowHandler) seedWorkItems(ctx context.Context, direction string) error {
	for _, caseType := range caseTypesForWorkItemDirection(direction) {
		cases, err := h.caseRepo.ListCases(ctx, repository.CaseListFilter{
			CaseType: caseType,
			Limit:    200,
		})
		if err != nil {
			return err
		}
		for _, item := range cases {
			if item.Status != repository.CaseStatusSubmitted && item.Status != repository.CaseStatusInReview {
				continue
			}
			seed, ok := workItemSeedFromCase(item)
			if !ok {
				continue
			}
			if _, err := h.caseRepo.UpsertWorkItem(ctx, seed); err != nil {
				return err
			}
		}
	}
	return nil
}

func workItemSeedFromCase(item repository.BusinessCase) (repository.WorkItemSeed, bool) {
	return repository.WorkItemSeed{}, false
}

func workItemFilter(r *http.Request) repository.WorkItemFilter {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	return repository.WorkItemFilter{
		Direction:         strings.ToUpper(q.Get("direction")),
		From:              parseDate(q.Get("from"), q.Get("fromDate")),
		To:                parseDate(q.Get("to"), q.Get("toDate")),
		Accounting:        strings.ToUpper(q.Get("accounting")),
		SLAStatus:         strings.ToUpper(firstString(q.Get("slaStatus"), q.Get("sla_status"))),
		TransactionStatus: strings.ToUpper(firstString(q.Get("transactionStatus"), q.Get("status"))),
		Node:              firstString(q.Get("node"), q.Get("step_code"), q.Get("currentStep")),
		Scope:             strings.ToUpper(q.Get("scope")),
		UserID:            currentUserID(r),
		Limit:             limit,
	}
}

func (h *WorkflowHandler) applyWorkItemPermissions(ctx context.Context, r *http.Request, items []repository.WorkItem) error {
	userID := currentUserID(r)
	for i := range items {
		makerTrack := userID != "" &&
			items[i].CreatedBy == userID &&
			repository.IsMakerTrackCaseType(items[i].CaseType)

		if items[i].AssignedTo != "" {
			items[i].CanClaim = false
			items[i].CanOpen = items[i].AssignedTo == userID || makerTrack
			if !items[i].CanOpen {
				items[i].ClaimBlockedReason = "Task đang được xử lý bởi " + items[i].AssignedTo
			}
			continue
		}
		ok, err := h.canClaimCandidateRole(r.Context(), r, items[i].CandidateRole)
		if err != nil {
			return err
		}
		items[i].CanClaim = ok
		items[i].CanOpen = ok || makerTrack
		if !ok && !makerTrack {
			items[i].ClaimBlockedReason = "Bạn không thuộc nhóm được phân công"
		}
	}
	return nil
}

func workItemSummary(items []repository.WorkItem, userID string) []repository.WorkItemSummaryNode {
	nodesByStep := map[string]*repository.WorkItemSummaryNode{}
	root := repository.WorkItemSummaryNode{ID: "ALL", Label: "Tất cả việc được phép nhận"}
	my := repository.WorkItemSummaryNode{ID: "MINE", Label: "Việc của tôi"}
	overdue := repository.WorkItemSummaryNode{ID: "SLA_BREACHED", Label: "Quá hạn SLA"}

	for _, item := range items {
		root.Count++
		if item.AssignedTo != "" && item.AssignedTo == userID {
			my.Count++
		}
		if item.SLAStatus == "BREACHED" {
			root.Overdue++
			overdue.Count++
			overdue.Overdue++
		}
		node := nodesByStep[item.StepCode]
		if node == nil {
			node = &repository.WorkItemSummaryNode{
				ID:    item.StepCode,
				Label: taskLabelForType(item.TaskType),
			}
			nodesByStep[item.StepCode] = node
		}
		node.Count++
		if item.SLAStatus == "BREACHED" {
			node.Overdue++
		}
	}

	out := []repository.WorkItemSummaryNode{root, my, overdue}
	for _, node := range nodesByStep {
		out = append(out, *node)
	}
	return out
}

func (h *WorkflowHandler) TaskByID(w http.ResponseWriter, r *http.Request) {
	jobKey, action := taskPath(r.URL.Path)
	if jobKey == 0 || action != "complete" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if h.zeebeSvc == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe service is not configured")
		return
	}
	var req struct {
		ProcessInstanceKey flexInt64      `json:"processInstanceKey"`
		ElementID          string         `json:"elementId"`
		Variables          map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	actor := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get("X-User-Email"))
	}
	elementID := normalizeUserTaskElementID(req.ElementID)
	decision := reviewDecisionFromVariables(req.Variables)
	comment := reviewCommentFromVariables(req.Variables)
	if elementID == "UT_CheckerReview" {
		if err := requireReviewComment(decision, comment); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.enforceMakerChecker(r, req.ProcessInstanceKey.Int64(), req.ElementID, actor); err != nil {
		slog.Warn("workflow task complete forbidden",
			"actor", actor,
			"jobKey", jobKey,
			"processInstanceKey", req.ProcessInstanceKey.Int64(),
			"elementId", req.ElementID,
			"err", err,
		)
		writeAPIError(w, r, http.StatusForbidden, err.Error())
		return
	}
	slog.Info("workflow task complete requested",
		"actor", actor,
		"jobKey", jobKey,
		"processInstanceKey", req.ProcessInstanceKey.Int64(),
		"elementId", req.ElementID,
	)
	if h.shouldUseNativeUserTaskComplete(r.Context(), req.ElementID, req.ProcessInstanceKey.Int64()) {
		if err := h.completeNativeUserTask(r.Context(), jobKey, req.ElementID, req.Variables, req.ProcessInstanceKey.Int64()); err != nil {
			slog.Error("workflow native user task complete failed",
				"actor", actor,
				"jobKey", jobKey,
				"processInstanceKey", req.ProcessInstanceKey.Int64(),
				"elementId", req.ElementID,
				"err", err,
			)
			writeAPIError(w, r, http.StatusBadGateway, "Failed to complete user task: "+err.Error())
			return
		}
	} else if err := h.zeebeSvc.CompleteTask(r.Context(), jobKey, req.Variables); err != nil {
		slog.Error("workflow task complete failed in zeebe",
			"actor", actor,
			"jobKey", jobKey,
			"processInstanceKey", req.ProcessInstanceKey.Int64(),
			"elementId", req.ElementID,
			"err", err,
		)
		writeAPIError(w, r, http.StatusBadGateway, "Failed to complete task: "+err.Error())
		return
	}
	if err := h.caseRepo.CompleteWorkItemByJob(r.Context(), jobKey); err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to update work item: "+err.Error())
		return
	}
	if err := h.caseRepo.MarkCaseStepCompleted(r.Context(), req.ProcessInstanceKey.Int64(), req.ElementID); err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to update completed task step: "+err.Error())
		return
	}
	h.seedEagerNextUserTask(r.Context(), req.ProcessInstanceKey.Int64(), req.ElementID, req.Variables)
	if elementID == "UT_CheckerReview" && decision != "" {
		var bc *repository.BusinessCase
		if req.ProcessInstanceKey.Int64() > 0 {
			bc, _ = h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), req.ProcessInstanceKey.Int64())
		}
		if bc != nil {
			h.recordCheckerDecisionTimeline(r.Context(), bc.ID, decision, comment, actor)
			h.notifyCheckerDecision(r.Context(), bc, jobKey, decision, comment)
		}
	}
	slog.Info("workflow task complete succeeded",
		"actor", actor,
		"jobKey", jobKey,
		"processInstanceKey", req.ProcessInstanceKey.Int64(),
		"elementId", req.ElementID,
	)
	writeJSON(w, r, http.StatusOK, map[string]any{"status": "completed"})
}

func (h *WorkflowHandler) ProcessInstanceByKey(w http.ResponseWriter, r *http.Request) {
	keyText, action := processInstancePath(r.URL.Path)
	if keyText == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "runtime":
		h.processInstanceRuntime(w, r, keyText)
	case r.Method == http.MethodPost && action == "retry-service-jobs":
		processInstanceKey, err := strconv.ParseInt(strings.TrimSpace(keyText), 10, 64)
		if err != nil || processInstanceKey <= 0 {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid process instance key")
			return
		}
		h.retryProcessServiceJobs(w, r, processInstanceKey)
	default:
		http.NotFound(w, r)
	}
}

func (h *WorkflowHandler) JobByKey(w http.ResponseWriter, r *http.Request) {
	jobKey, action := jobPath(r.URL.Path)
	if jobKey == 0 {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodPost && action == "retry":
		h.retryJob(w, r, jobKey)
	default:
		http.NotFound(w, r)
	}
}

func (h *WorkflowHandler) processInstanceRuntime(w http.ResponseWriter, r *http.Request, keyText string) {
	processInstanceKey, err := strconv.ParseInt(strings.TrimSpace(keyText), 10, 64)
	if err != nil || processInstanceKey <= 0 {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid process instance key")
		return
	}
	if h.zeebeSvc == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe service is not configured")
		return
	}

	runtimeCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	zeebeStatus := "ok"
	if err := h.zeebeSvc.HealthCheck(runtimeCtx); err != nil {
		zeebeStatus = "unreachable"
		slog.Warn("process runtime zeebe health failed",
			"processInstanceKey", processInstanceKey,
			"err", err,
		)
	}

	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(runtimeCtx, processInstanceKey)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query case: "+err.Error())
		return
	}

	var activeWorkTask any
	if bc != nil {
		if pending, err := h.caseRepo.FindActiveWorkTask(runtimeCtx, bc.ID, processInstanceKey); err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work task: "+err.Error())
			return
		} else if pending != nil {
			activeWorkTask = pending
		}
	}

	var pendingJobs []service.ProcessJobSnapshot
	var jobsErr string
	if zeebeStatus == "ok" {
		scanCtx, scanCancel := context.WithTimeout(r.Context(), 8*time.Second)
		caseType := ""
		currentStep := ""
		if bc != nil {
			caseType = bc.CaseType
			currentStep = bc.CurrentStep
		}
		jobs, err := h.zeebeSvc.FindProcessJobsForCase(scanCtx, processInstanceKey, caseType, currentStep)
		scanCancel()
		if len(jobs) > 0 {
			pendingJobs = jobs
		}
		if err != nil {
			jobsErr = err.Error()
			slog.Warn("process runtime pending jobs scan failed",
				"processInstanceKey", processInstanceKey,
				"caseType", caseType,
				"currentStep", currentStep,
				"jobsFound", len(pendingJobs),
				"err", err,
			)
		}
	}
	if pendingJobs == nil {
		pendingJobs = []service.ProcessJobSnapshot{}
	}

	var timeline []repository.TimelineEvent
	if bc != nil {
		if events, err := h.caseRepo.ListTimeline(runtimeCtx, bc.ID); err == nil {
			timeline = events
		}
	}
	if timeline == nil {
		timeline = []repository.TimelineEvent{}
	}
	incidents := incidentsFromTimeline(timeline)
	if incidents == nil {
		incidents = []service.ProcessIncidentSnapshot{}
	}
	activeElementID := activeElementID(bc, pendingJobs, incidents)

	writeJSON(w, r, http.StatusOK, map[string]any{
		"processInstanceKey": strconv.FormatInt(processInstanceKey, 10),
		"zeebeStatus":        zeebeStatus,
		"activeElementId":    activeElementID,
		"case":               bc,
		"activeWorkTask":     activeWorkTask,
		"pendingJobs":        pendingJobs,
		"incidents":          incidents,
		"pendingJobsError":   jobsErr,
		"timeline":           timeline,
		"hint":               runtimeHint(bc, pendingJobs, zeebeStatus),
		"workerNote":         "CRM/notification workers chạy trong workflow-service — xem log workflow-service, không phải crm-service HTTP.",
	})
}

func (h *WorkflowHandler) CaseByID(w http.ResponseWriter, r *http.Request) {
	id, action := casePath(r.URL.Path)
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		h.getCase(w, r, id)
	case r.Method == http.MethodGet && action == "timeline":
		h.caseTimeline(w, r, id)
	case r.Method == http.MethodPost && action == "submit":
		h.submitCase(w, r, id)
	case r.Method == http.MethodPost && action == "claim":
		h.claimCase(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func taskTypeForRequest(role string, taskType string) string {
	return ""
}

func (h *WorkflowHandler) listCases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	cases, err := h.caseRepo.ListCases(r.Context(), repository.CaseListFilter{
		CaseType:      q.Get("case_type"),
		Status:        q.Get("status"),
		AssignedTo:    q.Get("assigned_to"),
		CandidateRole: q.Get("candidate_role"),
		Keyword:       q.Get("keyword"),
		Limit:         limit,
	})
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query cases: "+err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, cases)
}

func (h *WorkflowHandler) createCase(w http.ResponseWriter, r *http.Request) {
	var req repository.CaseCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	bc, err := h.workflowCmd.CreateCase(r.Context(), req)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, bc)
}

func (h *WorkflowHandler) getCase(w http.ResponseWriter, r *http.Request, id string) {
	bc, err := h.caseRepo.GetCase(r.Context(), id)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query case: "+err.Error())
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "Case not found")
		return
	}
	writeJSON(w, r, http.StatusOK, bc)
}

func (h *WorkflowHandler) caseTimeline(w http.ResponseWriter, r *http.Request, id string) {
	events, err := h.caseRepo.ListTimeline(r.Context(), id)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query timeline: "+err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, events)
}

type caseActorRequest struct {
	Actor     string         `json:"actor"`
	Variables map[string]any `json:"variables"`
}

func (h *WorkflowHandler) submitCase(w http.ResponseWriter, r *http.Request, id string) {
	var req caseActorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	updated, err := h.workflowCmd.SubmitCase(r.Context(), id, service.SubmitCaseInput{
		Actor:     req.Actor,
		Variables: req.Variables,
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, r, http.StatusNotFound, "Case not found")
			return
		}
		writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, updated)
}

func (h *WorkflowHandler) claimCase(w http.ResponseWriter, r *http.Request, id string) {
	var req caseActorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	bc, err := h.caseRepo.ClaimCase(r.Context(), id, req.Actor)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "Case not found")
		return
	}
	writeJSON(w, r, http.StatusOK, bc)
}

func processInstancePath(path string) (string, string) {
	const prefix = "/api/workflow/process-instances/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func runtimeHint(bc *repository.BusinessCase, jobs []service.ProcessJobSnapshot, zeebeStatus string) string {
	if zeebeStatus != "ok" {
		return "Không kết nối được Zeebe gateway — kiểm tra zeebe_addr và mạng tới broker."
	}
	if bc == nil {
		return "Không tìm thấy business case trong DB cho process instance này."
	}
	step := strings.TrimSpace(bc.CurrentStep)
	if step == "" || step == "submitted" {
		return "DB vẫn ghi bước \"submitted\" — workflow có thể mới start hoặc CRM worker chưa chạy (mark_submitted → check_duplicate). Xem log workflow-service: workflow CRM job received. Nếu không có log, job service task trên Zeebe chưa được worker nhận hoặc đang bị lock."
	}
	if len(jobs) == 0 {
		return "Zeebe không quét được job pending cho process này (hoặc scan timeout). DB ghi bước \"" + bc.CurrentStep + "\". Process có thể giữa hai bước, đã hoàn tất, hoặc job đang bị worker khác giữ."
	}
	job := jobs[0]
	if strings.HasPrefix(job.JobType, "crm.") {
		return "Process đang chờ service task " + job.JobType + " tại " + job.ElementID + ". Worker CRM chạy trong workflow-service — nếu không thấy log workflow CRM job received thì job chưa được broker phân phối hoặc đang bị lock."
	}
	if strings.HasPrefix(job.JobType, "workflow.") {
		return "Có user task " + job.JobType + " tại " + job.ElementID + " (job " + strconv.FormatInt(job.JobKey, 10) + "). Thử Nhận & mở từ workbench hoặc claim qua API."
	}
	return "Job đang chờ: " + job.JobType + " tại " + job.ElementID
}

func casePath(path string) (string, string) {
	const prefix = "/api/workflow/cases/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func taskPath(path string) (int64, string) {
	const prefix = "/api/workflow/tasks/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return 0, ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return 0, ""
	}
	jobKey, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, ""
	}
	return jobKey, parts[1]
}

func caseTypePath(path string) (string, string) {
	const prefix = "/api/workflow/case-types/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func processDefinitionPath(path string) (string, string) {
	const prefix = "/api/workflow/process-definitions/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func workItemPath(path string) (string, string) {
	const prefix = "/api/workflow/work-items/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func parseProcessDefinitionImport(r *http.Request) (repository.ProcessDefinitionImport, error) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		return repository.ProcessDefinitionImport{}, err
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return repository.ProcessDefinitionImport{}, err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return repository.ProcessDefinitionImport{}, err
	}
	name := r.FormValue("name")
	resourceName := r.FormValue("resourceName")
	if resourceName == "" && header != nil {
		resourceName = header.Filename
	}
	return repository.ProcessDefinitionImport{
		ProcessCode:  r.FormValue("processCode"),
		Name:         name,
		ResourceName: resourceName,
		XMLContent:   string(content),
		Status:       r.FormValue("status"),
	}, nil
}

func safeFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	if name == "" {
		return "process.bpmn"
	}
	return name
}

func parseDate(values ...string) *time.Time {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			parsed, err := time.Parse(layout, value)
			if err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func currentUserID(r *http.Request) string {
	return firstString(
		strings.TrimSpace(r.Header.Get("X-User-Id")),
		strings.TrimSpace(r.Header.Get("X-User-Subject")),
	)
}

func currentUserGroups(r *http.Request) []string {
	raw := firstString(r.Header.Get("X-User-Group-Ids"), r.Header.Get("X-User-Groups"), r.Header.Get("X-Groups"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isSuperadminActor(r *http.Request) bool {
	return hasToken(currentUserPermissions(r), "superadmin") || hasToken(currentUserRoles(r), "SUPER_ADMIN")
}

func (h *WorkflowHandler) canClaimCandidateRole(ctx context.Context, r *http.Request, candidateRole string) (bool, error) {
	candidateRole = strings.TrimSpace(candidateRole)
	if candidateRole == "" {
		return false, nil
	}
	if isSuperadminActor(r) {
		return true, nil
	}
	if hasToken(currentUserRoles(r), candidateRole) || hasToken(currentUserPermissions(r), candidateRole) {
		return true, nil
	}
	userID := currentUserID(r)
	if userID == "" {
		return false, nil
	}
	return h.caseRepo.UserCanClaimRole(ctx, userID, currentUserGroups(r), candidateRole)
}

func userCanClaimCandidateRole(r *http.Request, candidateRole string) bool {
	candidateRole = strings.TrimSpace(candidateRole)
	if candidateRole == "" {
		return false
	}
	if isSuperadminActor(r) {
		return true
	}
	if hasToken(currentUserRoles(r), candidateRole) || hasToken(currentUserPermissions(r), candidateRole) {
		return true
	}
	return false
}

func currentUserRoles(r *http.Request) []string {
	return splitHeaderTokens(firstString(r.Header.Get("X-Roles"), r.Header.Get("X-User-Roles")))
}

func currentUserPermissions(r *http.Request) []string {
	return splitHeaderTokens(firstString(r.Header.Get("X-Permissions"), r.Header.Get("X-User-Permissions")))
}

func splitHeaderTokens(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func hasToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if strings.EqualFold(token, want) {
			return true
		}
	}
	return false
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (h *WorkflowHandler) seedEagerNextUserTask(ctx context.Context, processInstanceKey int64, completedElementID string, variables map[string]any) {
	if processInstanceKey == 0 || h.caseRepo == nil {
		return
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(ctx, processInstanceKey)
	if err != nil || bc == nil {
		if err != nil {
			slog.Warn("eager next task: load case failed", "processInstanceKey", processInstanceKey, "err", err)
		}
		return
	}
	task, ok := service.NextUserTaskAfterComplete(bc.CaseType, completedElementID, variables)
	if !ok {
		return
	}
	service.SeedEagerUserTask(ctx, h.caseRepo, bc, task)
}

func taskClaimFilterForWorkItem(ctx context.Context, caseRepo *repository.CaseRepository, item *repository.WorkItem) service.TaskClaimFilter {
	filter := service.TaskClaimFilter{
		CaseID:    item.CaseID,
		ElementID: item.StepCode,
	}
	if item.ProcessInstanceKey != nil {
		filter.ProcessInstanceKey = *item.ProcessInstanceKey
		return filter
	}
	bc, err := caseRepo.GetCase(ctx, item.CaseID)
	if err == nil && bc != nil && bc.ProcessInstanceKey != nil {
		filter.ProcessInstanceKey = *bc.ProcessInstanceKey
	}
	return filter
}

func int64Ptr(value int64) *int64 {
	return &value
}

func caseTypesForWorkItemDirection(direction string) []string {
	switch strings.ToUpper(direction) {
	case "INCOMING":
		return []string{"CUSTOMER_REGISTRATION", "CUSTOMER_ADJUSTMENT", "FINANCE_INCOMING_TRANSACTION", "HRM_EMPLOYEE_REGISTRATION"}
	case "OUTGOING":
		return []string{"FINANCE_OUTGOING_TRANSACTION"}
	default:
		return []string{"CUSTOMER_REGISTRATION", "CUSTOMER_ADJUSTMENT", "FINANCE_INCOMING_TRANSACTION", "FINANCE_OUTGOING_TRANSACTION", "HRM_EMPLOYEE_REGISTRATION"}
	}
}

func defaultStepForCaseType(caseType string) string {
	switch caseType {
	case "CUSTOMER_REGISTRATION":
		return "UT_CheckerReview"
	case "FINANCE_INCOMING_TRANSACTION":
		return "classify-account"
	case "FINANCE_OUTGOING_TRANSACTION":
		return "verify-beneficiary"
	case "HRM_EMPLOYEE_REGISTRATION":
		return "Activity_HRMReview"
	default:
		return "submitted"
	}
}

func roleForTaskType(taskType string) string {
	switch taskType {
	case "workflow.customer_checker_review":
		return "CUSTOMER_CHECKER"
	case "workflow.customer_risk_review":
		return "CUSTOMER_RISK_CHECKER"
	case "workflow.customer_maker_revise":
		return "CUSTOMER_MAKER"
	case "workflow.finance_incoming_classify", "workflow.finance_outgoing_verify":
		return "FINANCE_TXN_MAKER"
	case "workflow.finance_incoming_approve", "workflow.finance_outgoing_approve":
		return "FINANCE_TXN_CHECKER"
	case "workflow.hrm_registration_review":
		return "HRM_REGISTRATION_REVIEWER"
	case "workflow.hrm_registration_approve":
		return "HRM_REGISTRATION_APPROVER"
	default:
		return ""
	}
}

func taskLabelForType(taskType string) string {
	switch taskType {
	case "workflow.customer_checker_review":
		return "Kiểm soát hồ sơ khách hàng"
	case "workflow.customer_risk_review":
		return "Rà soát rủi ro khách hàng"
	case "workflow.customer_maker_revise":
		return "Maker bổ sung hồ sơ"
	case "workflow.finance_incoming_classify":
		return "Phân loại giao dịch đến"
	case "workflow.finance_incoming_approve":
		return "Duyệt giao dịch đến"
	case "workflow.finance_outgoing_verify":
		return "Kiểm tra giao dịch đi"
	case "workflow.finance_outgoing_approve":
		return "Duyệt giao dịch đi"
	case "workflow.hrm_registration_review":
		return "Kiem tra ho so nhan su"
	case "workflow.hrm_registration_approve":
		return "Phe duyet tiep nhan nhan su"
	default:
		return taskType
	}
}

var checkerTaskSteps = map[string]struct{}{
	"Activity_CheckerReview": {},
	"Activity_RiskReview":    {},
}

func (h *WorkflowHandler) enforceMakerChecker(r *http.Request, processInstanceKey int64, elementID, actor string) error {
	if processInstanceKey == 0 || elementID == "" || actor == "" {
		return nil
	}
	if _, ok := checkerTaskSteps[elementID]; !ok {
		return nil
	}
	if isSuperadminActor(r) || makerCheckerSODRelaxed() {
		return nil
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), processInstanceKey)
	if err != nil || bc == nil {
		return nil
	}
	if bc.CreatedBy != "" && bc.CreatedBy == actor {
		return errors.New("maker cannot complete checker task — hồ sơ do chính bạn tạo/trình (tách nhiệm maker-checker). Đăng nhập user CUSTOMER_CHECKER khác, hoặc dev: WORKFLOW_RELAX_MAKER_CHECKER_SOD=true / superadmin")
	}
	return nil
}

func makerCheckerSODRelaxed() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("WORKFLOW_RELAX_MAKER_CHECKER_SOD")), "true")
}

func writeListOrError(w http.ResponseWriter, r *http.Request, items any, err error) {
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []any{}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

func writeMutationOrError(w http.ResponseWriter, r *http.Request, item any, err error) {
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, item)
}

func writeUpdateOrError(w http.ResponseWriter, r *http.Request, item any, err error, notFound string) {
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if item == nil {
		writeAPIError(w, r, http.StatusNotFound, notFound)
		return
	}
	writeJSON(w, r, http.StatusOK, item)
}

func writeDeleteOrError(w http.ResponseWriter, r *http.Request, deleted bool, err error, notFound string) {
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if !deleted {
		writeAPIError(w, r, http.StatusNotFound, notFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeDeployError(w http.ResponseWriter, r *http.Request, err error) {
	statusCode := http.StatusInternalServerError
	if status.Code(err) == codes.InvalidArgument {
		statusCode = http.StatusBadRequest
	}
	writeAPIError(w, r, statusCode, "Failed to deploy: "+err.Error())
}
