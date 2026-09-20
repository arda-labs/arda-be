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
	ardaexport "github.com/arda-labs/arda/libs/go/arda-export"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WorkflowHandler struct {
	zeebeSvc           *service.ZeebeService
	zeebeRest          *service.ZeebeRestClient
	notificationClient *notificationclient.Client
	workflowCmd        *service.WorkflowCommandService
	mappingRepo        *repository.MappingRepository
	caseRepo           *repository.CaseRepository
	processDefinition  *repository.ProcessDefinitionRepository
	// AssignmentResolver and IncidentIndex are optional integrations wired
	// from main; both nil-safe (features degrade instead of failing).
	AssignmentResolver *service.AssignmentResolver
	IncidentIndex      *service.ZeebeIncidentIndex
	// MonitoringIndex is the Zeebe exporter read model behind the operate
	// search/detail endpoints (Camunda 8.5 has no Operate and no REST search).
	MonitoringIndex *service.ZeebeMonitoringIndex
	// aiStoreOverride swaps the store behind the internal AI surface
	// (/internal/ai/*) — tests inject a fake; production leaves it nil so
	// caseRepo is used.
	aiStoreOverride AIWorkflowStore
	// caseScopeOverride swaps the tenant-scoped registry behind Zeebe-key
	// authorization (workflow_case_scope.go) — tests inject a fake; production
	// leaves it nil so caseRepo is used.
	caseScopeOverride workflowCaseScope
}

func NewWorkflowHandler(
	zeebeSvc *service.ZeebeService,
	zeebeRest *service.ZeebeRestClient,
	mappingRepo *repository.MappingRepository,
	caseRepo *repository.CaseRepository,
	processDefinition *repository.ProcessDefinitionRepository,
) *WorkflowHandler {
	return &WorkflowHandler{
		zeebeSvc:          zeebeSvc,
		zeebeRest:         zeebeRest,
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

	writeJSON(w, r, http.StatusOK, map[string]any{
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

	writeJSON(w, r, http.StatusOK, map[string]any{
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

	writeJSON(w, r, http.StatusOK, map[string]any{
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
	if h.zeebeSvc == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe service is not configured")
		return
	}

	// Cancelling a process instance mutates the shared Zeebe cluster; the
	// instance must first resolve to a case owned by the caller tenant.
	bc, err := h.caseForProcessInstanceKey(r.Context(), instanceKey)
	if err != nil {
		writeCaseScopeError(w, r, err)
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "Process instance not found")
		return
	}

	err = h.zeebeSvc.CancelWorkflow(r.Context(), instanceKey)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to cancel workflow: "+err.Error())
		return
	}

	writeJSON(w, r, http.StatusOK, map[string]any{
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

	writeJSON(w, r, http.StatusOK, mapping)
}

func (h *WorkflowHandler) CaseTypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Catalog lookup: unpaged (table < ~500 rows) but q-searchable and
		// sortable via the shared list contract.
		q := ardahttp.ParseListQuery(r.URL.Query())
		caseTypes, err := h.caseRepo.ListCaseTypes(r.Context(), q.Q, q.Sort, q.Order)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to query case types: "+err.Error())
			return
		}
		ardahttp.WriteEnvelopeUnpaged(w, r, caseTypes)
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
		return
	}
	if r.Method == http.MethodGet && action == "steps" {
		h.caseTypeSteps(w, r, caseType)
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

// caseTypeSteps exposes the registry step metadata (allowed actions, form key,
// comment requirements) that the shared task UI renders from.
func (h *WorkflowHandler) caseTypeSteps(w http.ResponseWriter, r *http.Request, caseType string) {
	version := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("version")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			version = parsed
		}
	}
	if version == 0 {
		v, err := h.caseRepo.CaseTypeRegistryVersion(r.Context(), caseType)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		version = v
	}
	steps, err := h.caseRepo.ListCaseTypeSteps(r.Context(), caseType, version)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"caseType":        caseType,
		"registryVersion": version,
		"steps":           steps,
	})
}

func (h *WorkflowHandler) SLAPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := ardahttp.ParseListQuery(r.URL.Query())
		items, err := h.caseRepo.ListSLAPolicies(r.Context(), q.Q, q.Sort, q.Order)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		ardahttp.WriteEnvelopeUnpaged(w, r, items)
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
		q := ardahttp.ParseListQuery(r.URL.Query())
		items, err := h.caseRepo.ListDescriptionTemplates(r.Context(), q.Q, q.Sort, q.Order)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		ardahttp.WriteEnvelopeUnpaged(w, r, items)
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	tenantID, err := requiredWorkflowTargetTenant(r)
	if err != nil {
		writeAPIError(w, r, http.StatusForbidden, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowRoleMemberships(r.Context(), tenantID)
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.WorkflowRoleMembership
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateWorkflowRoleMembership(r.Context(), tenantID, req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) RoleMembershipByID(w http.ResponseWriter, r *http.Request) {
	tenantID, err := requiredWorkflowTargetTenant(r)
	if err != nil {
		writeAPIError(w, r, http.StatusForbidden, err.Error())
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/role-memberships/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	item, err := h.caseRepo.UpdateWorkflowRoleMembership(r.Context(), tenantID, id, req)
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	tenantID, err := requiredWorkflowTargetTenant(r)
	if err != nil {
		writeAPIError(w, r, http.StatusForbidden, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowDelegations(r.Context(), tenantID)
		writeListOrError(w, r, items, err)
	case http.MethodPost:
		var req repository.WorkflowDelegation
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		item, err := h.caseRepo.CreateWorkflowDelegation(r.Context(), tenantID, req)
		writeMutationOrError(w, r, item, err)
	default:
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *WorkflowHandler) DelegationByID(w http.ResponseWriter, r *http.Request) {
	tenantID, err := requiredWorkflowTargetTenant(r)
	if err != nil {
		writeAPIError(w, r, http.StatusForbidden, err.Error())
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/delegations/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	item, err := h.caseRepo.UpdateWorkflowDelegation(r.Context(), tenantID, id, req)
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

// CompleteUserTask completes a native BPMN user task (Zeebe REST) or falls
// back to the job-based path. CRM v2 registration/adjustment and the
// workbench both post here.
func (h *WorkflowHandler) CompleteUserTask(w http.ResponseWriter, r *http.Request) {
	jobKey, action := taskPath(r.URL.Path)
	if jobKey == 0 || action != "complete" {
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	if actor == "" {
		writeAPIError(w, r, http.StatusUnauthorized, "verified actor is required")
		return
	}
	elementID := normalizeUserTaskElementID(req.ElementID)
	// Resolve the authoritative task scope and authorize the actor before any
	// state change. The caller-supplied processInstanceKey is never trusted.
	processInstanceKey, err := h.authorizeUserTaskComplete(r, jobKey, req.ProcessInstanceKey.Int64(), elementID, actor)
	if err != nil {
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
	variables := withNormalizedDecision(req.Variables)
	decision := reviewDecisionFromVariables(variables)
	comment := reviewCommentFromVariables(variables)
	if elementID == "UT_CheckerReview" {
		if err := requireReviewComment(decision, comment); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.enforceMakerChecker(r, processInstanceKey, elementID, actor); err != nil {
		slog.Warn("workflow task complete forbidden",
			"actor", actor,
			"jobKey", jobKey,
			"processInstanceKey", processInstanceKey,
			"elementId", elementID,
			"err", err,
		)
		writeAPIError(w, r, http.StatusForbidden, err.Error())
		return
	}
	// Record the decision durably before any engine/domain side effect so the
	// command can be reconciled if the engine call fails midway.
	if err := h.recordTaskDecision(r, jobKey, processInstanceKey, elementID, actor, comment, variables); err != nil {
		slog.Error("workflow task decision record failed",
			"actor", actor,
			"jobKey", jobKey,
			"processInstanceKey", processInstanceKey,
			"elementId", elementID,
			"err", err,
		)
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to record task decision: "+err.Error())
		return
	}
	slog.Info("workflow task complete requested",
		"actor", actor,
		"jobKey", jobKey,
		"processInstanceKey", processInstanceKey,
		"elementId", elementID,
	)
	if h.shouldUseNativeUserTaskComplete(r.Context(), elementID, processInstanceKey) {
		if err := h.completeNativeUserTask(r.Context(), jobKey, elementID, variables, processInstanceKey); err != nil {
			slog.Error("workflow native user task complete failed",
				"actor", actor,
				"jobKey", jobKey,
				"processInstanceKey", processInstanceKey,
				"elementId", elementID,
				"err", err,
			)
			writeAPIError(w, r, http.StatusBadGateway, "Failed to complete user task: "+err.Error())
			return
		}
	} else if err := h.zeebeSvc.CompleteTask(r.Context(), jobKey, variables); err != nil {
		slog.Error("workflow task complete failed in zeebe",
			"actor", actor,
			"jobKey", jobKey,
			"processInstanceKey", processInstanceKey,
			"elementId", elementID,
			"err", err,
		)
		writeAPIError(w, r, http.StatusBadGateway, "Failed to complete task: "+err.Error())
		return
	}
	if err := h.caseRepo.CompleteWorkItemByJob(r.Context(), jobKey); err != nil {
		slog.Warn("workflow task work item completion failed",
			"jobKey", jobKey, "processInstanceKey", processInstanceKey, "err", err)
	}
	// Timeline + notification: checker decisions get their semantic event
	// types; maker submits and other completions get a generic TASK_COMPLETED.
	if bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), processInstanceKey); err == nil && bc != nil {
		if eventType := checkerTimelineEventType(decision); eventType != "" {
			h.recordCheckerDecisionTimeline(r.Context(), bc.ID, decision, comment, actor)
			h.notifyCheckerDecision(r.Context(), bc, jobKey, decision, comment)
		} else {
			note := elementID
			if decision != "" {
				note = elementID + " — " + decision
			}
			if err := h.caseRepo.AddTimelineEvent(r.Context(), bc.ID, "TASK_COMPLETED", note); err != nil {
				slog.Warn("failed to record task completion timeline", "caseId", bc.ID, "err", err)
			}
		}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"status": "completed"})
}

// recordTaskDecision stores the human decision before the engine/domain side
// effects. Unknown/unprojected tasks (no local work item yet) are skipped with
// a warning: the claim path persists the work item, and legacy job tasks have
// no activation row to attach the decision to.
func (h *WorkflowHandler) recordTaskDecision(r *http.Request, jobKey, processInstanceKey int64, elementID, actor, comment string, variables map[string]any) error {
	if h.caseRepo == nil || jobKey == 0 {
		return nil
	}
	item, err := h.caseRepo.FindWorkItemByJobKey(r.Context(), jobKey)
	if err != nil {
		return err
	}
	if item == nil {
		slog.Warn("workflow task decision not recorded: work item not found",
			"jobKey", jobKey, "processInstanceKey", processInstanceKey, "elementId", elementID)
		return nil
	}
	decision := recordedDecision(elementID, variables)
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		key = fmt.Sprintf("task-%d-%s", jobKey, decision)
	}
	dataVersion, _ := variables["dataVersion"].(string)
	// The dispatcher has no job context, so the tenant/org scope the completing
	// request carried is recorded here and rebuilt as outgoing metadata later.
	scope := ardametadata.FromHTTPHeaders(r.Header)
	if scope.TenantID == "" {
		scope.TenantID = strings.TrimSpace(ardametadata.FromIncoming(r.Context()).TenantID)
	}
	orgID := scope.OrgID
	if orgID == "" && len(scope.OrgIDs) > 0 {
		orgID = scope.OrgIDs[0]
	}
	_, err = h.caseRepo.InsertTaskDecision(r.Context(), repository.TaskDecision{
		TaskID:             item.ID,
		CaseID:             item.CaseID,
		ProcessInstanceKey: processInstanceKey,
		ElementID:          elementID,
		Decision:           decision,
		Comment:            comment,
		Actor:              actor,
		DataVersion:        strings.TrimSpace(dataVersion),
		TenantID:           strings.TrimSpace(scope.TenantID),
		OrgID:              strings.TrimSpace(orgID),
		IdempotencyKey:     key,
	})
	return err
}

// authorizeUserTaskComplete resolves the workflow registry entry for a job or
// native user-task key and checks that the actor may complete it:
// assigned user or member of the candidate role/group, or superadmin. It
// returns the process instance key recorded by the registry so maker-checker
// cannot be pointed at another case. Unknown/unprojected tasks are rejected
// unless the task key provably belongs to the claimed process instance.
func (h *WorkflowHandler) authorizeUserTaskComplete(r *http.Request, jobKey, claimedProcessKey int64, elementID, actor string) (int64, error) {
	if h.caseRepo == nil {
		return 0, errors.New("workflow registry is unavailable")
	}
	item, err := h.caseRepo.FindWorkItemByJobKey(r.Context(), jobKey)
	if err != nil {
		return 0, err
	}
	if item != nil {
		if item.Status == repository.TaskStatusCompleted || item.Status == repository.TaskStatusCancelled {
			return 0, fmt.Errorf("task is already %s", item.Status)
		}
		if service.IsNativeUserTaskElement(elementID) && strings.TrimSpace(item.StepCode) != "" &&
			normalizeUserTaskElementID(item.StepCode) != elementID {
			return 0, errors.New("task element does not match the job key")
		}
		if item.ProcessInstanceKey == nil || *item.ProcessInstanceKey <= 0 {
			return 0, errors.New("task has no process instance scope")
		}
		if isSuperadminActor(r) {
			return *item.ProcessInstanceKey, nil
		}
		if item.AssignedTo != "" {
			if item.AssignedTo != actor {
				return 0, errors.New("task is assigned to another user")
			}
			return *item.ProcessInstanceKey, nil
		}
		ok, err := h.canClaimCandidateRole(r.Context(), r, item.TenantID, item.CandidateRole)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, errors.New("user is not in the candidate role/group for this task")
		}
		return *item.ProcessInstanceKey, nil
	}

	// No local projection yet (native user task between projector sweeps). The
	// task key must belong to the claimed process instance, which verifies the
	// process scope even though the caller supplied it.
	if claimedProcessKey <= 0 || h.zeebeRest == nil || !h.zeebeRest.Enabled() || !service.IsNativeUserTaskElement(elementID) {
		return 0, errors.New("task scope could not be verified")
	}
	tasks, err := h.zeebeRest.SearchUserTasks(r.Context(), claimedProcessKey, "")
	if err != nil {
		return 0, fmt.Errorf("task scope could not be verified: %w", err)
	}
	for _, ut := range tasks {
		if ut.UserTaskKey != jobKey {
			continue
		}
		if normalizeUserTaskElementID(ut.ElementID) != elementID {
			return 0, errors.New("task element does not match the job key")
		}
		if ut.State != "" && !strings.EqualFold(ut.State, "CREATED") {
			return 0, fmt.Errorf("task is %s", ut.State)
		}
		bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), ut.ProcessInstanceKey)
		if err != nil || bc == nil {
			return 0, errors.New("task scope could not be verified")
		}
		if isSuperadminActor(r) {
			return ut.ProcessInstanceKey, nil
		}
		if ut.Assignee != "" {
			if ut.Assignee != actor {
				return 0, errors.New("task is assigned to another user")
			}
			return ut.ProcessInstanceKey, nil
		}
		ok, err := h.canClaimCandidateRole(r.Context(), r, bc.TenantID, firstCandidateGroup(ut.CandidateGroups))
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, errors.New("user is not in the candidate role/group for this task")
		}
		return ut.ProcessInstanceKey, nil
	}
	return 0, errors.New("task scope could not be verified")
}

// ClaimTask claims a workflow task by context (process instance / case /
// element) for domain remotes that run their approval inside the business
// page (CRM v2 flow). Native userTask claim only — the legacy parked
// runtime stays removed.
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
	filter := service.TaskClaimFilter{
		ProcessInstanceKey: req.ProcessInstanceKey.Int64(),
		CaseID:             strings.TrimSpace(req.CaseID),
		ElementID:          strings.TrimSpace(req.ElementID),
	}
	actor := currentUserID(r)
	slog.Info("workflow task claim requested",
		"actor", actor,
		"role", req.Role,
		"caseId", filter.CaseID,
		"elementId", filter.ElementID,
		"processInstanceKey", filter.ProcessInstanceKey,
	)

	claimCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	task, err := h.tryNativeUserTaskClaim(claimCtx, r, filter, filter.ElementID, actor)
	if err != nil {
		if errors.Is(err, errTaskNotAuthorized) {
			writeAPIError(w, r, http.StatusForbidden, err.Error())
			return
		}
		slog.Warn("native user task claim failed", "err", err)
		writeJSON(w, r, http.StatusBadGateway, map[string]any{
			"error": nativeClaimUnavailableMessage(filter, err),
		})
		return
	}
	if task == nil {
		writeJSON(w, r, http.StatusNotFound, map[string]any{
			"error": nativeClaimUnavailableMessage(filter, fmt.Errorf("no active native user task for element %q", filter.ElementID)),
		})
		return
	}
	h.persistInboxClaim(r.Context(), *task)
	writeJSON(w, r, http.StatusOK, task)
}

func (h *WorkflowHandler) WorkItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	items, err := h.caseRepo.ListWorkItems(r.Context(), workItemFilter(r))
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work items: "+err.Error())
		return
	}
	// Incoming (or omitted direction) only: filter to items the user can claim
	// or is assigned to. Search (ALL) and outgoing must not use this gate —
	// otherwise search looks empty when the caller omitted/lost direction=ALL.
	items, err = h.permissionFilteredWorkItems(r.Context(), r, items)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to resolve task permissions: "+err.Error())
		return
	}
	h.decorateWorkItemsWithRegistry(r.Context(), items)
	writeListAny(w, r, items)
}

// decorateWorkItemsWithRegistry attaches the registry step metadata (step
// kind, form key, allowed actions, comment requirements) so the shared task UI
// renders from server metadata instead of hardcoded case-type maps.
func (h *WorkflowHandler) decorateWorkItemsWithRegistry(ctx context.Context, items []repository.WorkItem) {
	if h.caseRepo == nil || len(items) == 0 {
		return
	}
	caseIDs := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.CaseID]; ok {
			continue
		}
		seen[item.CaseID] = struct{}{}
		caseIDs = append(caseIDs, item.CaseID)
	}
	versions, err := h.caseRepo.CaseRegistryVersions(ctx, caseIDs)
	if err != nil {
		slog.Warn("work item registry decoration skipped", "err", err)
		return
	}
	stepCache := make(map[string]map[string]repository.CaseTypeStep)
	for i := range items {
		version := versions[items[i].CaseID]
		if version <= 0 {
			version = 1
		}
		cacheKey := items[i].CaseType + ":" + strconv.Itoa(version)
		steps, ok := stepCache[cacheKey]
		if !ok {
			list, err := h.caseRepo.ListCaseTypeSteps(ctx, items[i].CaseType, version)
			if err != nil {
				slog.Warn("work item registry decoration failed",
					"caseType", items[i].CaseType, "registryVersion", version, "err", err)
				continue
			}
			steps = make(map[string]repository.CaseTypeStep, len(list))
			for _, step := range list {
				steps[step.ElementID] = step
			}
			stepCache[cacheKey] = steps
		}
		step, ok := steps[items[i].StepCode]
		if !ok {
			continue
		}
		items[i].StepKind = step.StepKind
		items[i].FormKey = step.FormKey
		items[i].AllowedActions = step.AllowedActions
		items[i].RequiredCommentOn = step.RequiredCommentOn
		items[i].RegistryVersion = version
	}
}

func (h *WorkflowHandler) WorkItemSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	filter := workItemFilter(r)
	filter.Limit = 200
	items, err := h.caseRepo.ListWorkItems(r.Context(), filter)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work item summary: "+err.Error())
		return
	}
	items, err = h.permissionFilteredWorkItems(r.Context(), r, items)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to resolve task permissions: "+err.Error())
		return
	}
	h.decorateWorkItemsWithRegistry(r.Context(), items)
	writeJSON(w, r, http.StatusOK, map[string]any{"nodes": workItemSummary(items, currentUserID(r))})
}

// Analytics handles GET /api/workflow/analytics/overview (W6 dashboard).
func (h *WorkflowHandler) Analytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeAPIError(w, r, http.StatusForbidden, "tenant scope is required")
		return
	}
	overview, err := h.caseRepo.AnalyticsOverview(r.Context(), tenantID,
		r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query analytics: "+err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, overview)
}

// ExportWorkItems handles GET /api/workflow/work-items/export — streams the
// filtered work-item list as XLSX (W6).
func (h *WorkflowHandler) ExportWorkItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	filter := workItemFilter(r)
	filter.Limit = 5000
	items, err := h.caseRepo.ListWorkItems(r.Context(), filter)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work items: "+err.Error())
		return
	}
	// The export must never leak more than the list endpoint: reuse the exact
	// same permission gate (including the default INCOMING direction).
	before := len(items)
	items, err = h.permissionFilteredWorkItems(r.Context(), r, items)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to resolve task permissions: "+err.Error())
		return
	}
	if hidden := before - len(items); hidden > 0 {
		slog.Info("export work items: rows hidden by task permissions",
			"hidden", hidden, "visible", len(items), "user", currentUserID(r))
	}
	index := 0
	supplier := func() ([]any, error) {
		if index >= len(items) {
			return nil, io.EOF
		}
		item := items[index]
		index++
		return []any{
			item.CaseCode, item.CaseType, item.Title, item.StepCode, item.Status,
			item.CandidateRole, item.AssignedToName, item.TransactionStatus,
		}, nil
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\"work-items.xlsx\"")
	if err := ardaexport.StreamXLSX(r.Context(), w, ardaexport.StreamOptions{
		Title:     "Work items",
		SheetName: "WorkItems",
		Columns: []ardaexport.Column{
			{Header: "Case", Key: "case", Type: ardaexport.CellTypeCode},
			{Header: "Case type", Key: "case_type", Type: ardaexport.CellTypeString},
			{Header: "Title", Key: "title", Type: ardaexport.CellTypeString},
			{Header: "Step", Key: "step", Type: ardaexport.CellTypeString},
			{Header: "Status", Key: "status", Type: ardaexport.CellTypeString},
			{Header: "Role", Key: "role", Type: ardaexport.CellTypeString},
			{Header: "Assignee", Key: "assignee", Type: ardaexport.CellTypeString},
			{Header: "Txn status", Key: "txn_status", Type: ardaexport.CellTypeString},
		},
		TotalCount: len(items),
	}, supplier); err != nil {
		slog.Error("export work items failed", "err", err)
	}
}

func (h *WorkflowHandler) WorkItemByID(w http.ResponseWriter, r *http.Request) {
	id, action := workItemPath(r.URL.Path)
	if id == "" {
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
		items := []repository.WorkItem{*item}
		h.decorateWorkItemsWithRegistry(r.Context(), items)
		writeJSON(w, r, http.StatusOK, items[0])
		return
	}
	if action != "claim" {
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	if item.Status != repository.TaskStatusReady || item.JobKey == nil {
		writeAPIError(w, r, http.StatusConflict, "Task is still being prepared and cannot be claimed")
		return
	}
	ok, err := h.canClaimCandidateRole(r.Context(), r, item.TenantID, item.CandidateRole)
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
			if native, nativeErr := h.tryNativeUserTaskClaim(claimCtx, r, filter, filter.ElementID, userID); nativeErr != nil {
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
	// The claim response must carry the same registry step metadata as the list
	// and by-id reads: the workbench form host resolves the task form by
	// `formKey` immediately after claiming (no second round-trip).
	claimedItems := []repository.WorkItem{*claimed}
	h.decorateWorkItemsWithRegistry(r.Context(), claimedItems)
	writeJSON(w, r, http.StatusOK, map[string]any{"workItem": claimedItems[0], "claimedBy": userID, "claimedAt": time.Now()})
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

func workItemFilter(r *http.Request) repository.WorkItemFilter {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	return repository.WorkItemFilter{
		Direction:         strings.ToUpper(q.Get("direction")),
		From:              parseDate(ardatime.TZ(r.Context()), q.Get("from"), q.Get("fromDate")),
		To:                parseDate(ardatime.TZ(r.Context()), q.Get("to"), q.Get("toDate")),
		Accounting:        strings.ToUpper(q.Get("accounting")),
		SLAStatus:         strings.ToUpper(firstString(q.Get("slaStatus"), q.Get("sla_status"))),
		TransactionStatus: strings.ToUpper(firstString(q.Get("transactionStatus"), q.Get("status"))),
		Node:              firstString(q.Get("node"), q.Get("step_code"), q.Get("currentStep")),
		Domain:            strings.ToUpper(firstString(q.Get("domain"), q.Get("business_area"))),
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

		if items[i].Status == repository.TaskStatusRouting {
			roleOK, err := h.canClaimCandidateRole(ctx, r, items[i].TenantID, items[i].CandidateRole)
			if err != nil {
				return err
			}
			items[i].CanView = roleOK || makerTrack
			items[i].CanClaim = false
			items[i].CanOpen = false
			if roleOK {
				items[i].ClaimBlockedReason = "Task đang được chuẩn bị"
			} else if makerTrack {
				items[i].ClaimBlockedReason = "Task đang được chuẩn bị, vui lòng đợi trong giây lát"
			} else {
				items[i].ClaimBlockedReason = "Bạn không thuộc nhóm được phân công"
			}
			continue
		}

		if items[i].AssignedTo != "" {
			items[i].CanClaim = false
			items[i].CanOpen = items[i].AssignedTo == userID || makerTrack
			items[i].CanView = items[i].CanOpen
			if !items[i].CanOpen {
				items[i].ClaimBlockedReason = "Task đang được xử lý bởi " + items[i].AssignedTo
			}
			continue
		}
		ok, err := h.canClaimCandidateRole(r.Context(), r, items[i].TenantID, items[i].CandidateRole)
		if err != nil {
			return err
		}
		items[i].CanClaim = ok
		items[i].CanOpen = ok || makerTrack
		items[i].CanView = items[i].CanClaim || items[i].CanOpen
		if !ok && !makerTrack {
			items[i].ClaimBlockedReason = "Bạn không thuộc nhóm được phân công"
		}
	}
	return nil
}

func visibleIncomingWorkItems(items []repository.WorkItem) []repository.WorkItem {
	visible := items[:0]
	for _, item := range items {
		if item.CanView {
			visible = append(visible, item)
		}
	}
	return visible
}

// permissionFilteredWorkItems is the shared read gate for every work-item
// surface (list, summary, export): per-item permissions are always resolved,
// and the default/INCOMING direction is additionally filtered to rows the
// caller may view. Keeping list and export on the same helper guarantees the
// export can never return rows the list would hide.
func (h *WorkflowHandler) permissionFilteredWorkItems(ctx context.Context, r *http.Request, items []repository.WorkItem) ([]repository.WorkItem, error) {
	if err := h.applyWorkItemPermissions(ctx, r, items); err != nil {
		return nil, err
	}
	dir := strings.ToUpper(r.URL.Query().Get("direction"))
	if dir == "" || dir == "INCOMING" {
		items = visibleIncomingWorkItems(items)
	}
	return items, nil
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

func (h *WorkflowHandler) ProcessInstanceByKey(w http.ResponseWriter, r *http.Request) {
	keyText, action := processInstancePath(r.URL.Path)
	if keyText == "" {
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
	}
}

func (h *WorkflowHandler) JobByKey(w http.ResponseWriter, r *http.Request) {
	jobKey, action := jobPath(r.URL.Path)
	if jobKey == 0 {
		writeAPIError(w, r, http.StatusNotFound, "route not found")
		return
	}
	switch {
	case r.Method == http.MethodPost && action == "retry":
		h.retryJob(w, r, jobKey)
	default:
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	if bc == nil {
		// The Zeebe job scan below is keyed by process instance only; without a
		// caller-tenant case the instance (and its jobs) must not be exposed.
		writeAPIError(w, r, http.StatusNotFound, "Process instance not found")
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
		writeAPIError(w, r, http.StatusNotFound, "route not found")
		return
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		h.getCase(w, r, id)
	case r.Method == http.MethodGet && action == "timeline":
		h.caseTimeline(w, r, id)
	case r.Method == http.MethodGet && action == "task-readiness":
		h.caseTaskReadiness(w, r, id)
	case r.Method == http.MethodGet && action == "variables":
		h.caseVariables(w, r, id)
	case r.Method == http.MethodPost && action == "submit":
		h.submitCase(w, r, id)
	case r.Method == http.MethodPost && action == "claim":
		h.claimCase(w, r, id)
	default:
		writeAPIError(w, r, http.StatusNotFound, "route not found")
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
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	// createdBy is the verified caller, never a client claim. A superadmin may
	// still create on behalf of another actor explicitly; anyone else sending a
	// different createdBy is rejected (fail-closed) so case ownership and maker
	// tracking cannot be forged.
	actor, err := authenticatedActorForCase(r, req.CreatedBy)
	if err != nil {
		writeCaseActorError(w, r, err)
		return
	}
	req.CreatedBy = actor

	bc, err := h.workflowCmd.CreateCase(r.Context(), req)
	if err != nil {
		if errors.Is(err, repository.ErrIdempotencyConflict) {
			writeAPIError(w, r, http.StatusConflict, err.Error())
			return
		}
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

// caseVariables serves GET /api/workflow/cases/{id}/variables: the live
// Zeebe process-instance variables of the case (formation screens read the
// appraisal/approval stage data plus amount/fundSource here).
func (h *WorkflowHandler) caseVariables(w http.ResponseWriter, r *http.Request, id string) {
	bc, err := h.caseRepo.GetCase(r.Context(), id)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query case: "+err.Error())
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "Case not found")
		return
	}
	if bc.ProcessInstanceKey == nil || *bc.ProcessInstanceKey <= 0 {
		writeAPIError(w, r, http.StatusConflict, "Case has no process instance yet")
		return
	}
	if h.zeebeRest == nil || !h.zeebeRest.Enabled() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe REST client is not configured")
		return
	}
	variables, err := h.zeebeRest.GetVariables(r.Context(), *bc.ProcessInstanceKey)
	if err != nil {
		writeAPIError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"case_id":              bc.ID,
		"process_instance_key": strconv.FormatInt(*bc.ProcessInstanceKey, 10),
		"variables":            variables,
	})
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
	Actor          string         `json:"actor"`
	Variables      map[string]any `json:"variables"`
	IdempotencyKey string         `json:"idempotencyKey"`
}

func (h *WorkflowHandler) caseTaskReadiness(w http.ResponseWriter, r *http.Request, id string) {
	stepCode := r.URL.Query().Get("stepCode")
	if stepCode == "" {
		writeAPIError(w, r, http.StatusBadRequest, "stepCode query param is required")
		return
	}
	item, err := h.caseRepo.FindPendingWorkItemByStep(r.Context(), id, stepCode)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work item: "+err.Error())
		return
	}
	ready := false
	status := "NOT_FOUND"
	if item != nil {
		status = string(item.Status)
		ready = item.Status == repository.TaskStatusReady || item.Status == repository.TaskStatusClaimed
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"ready":  ready,
		"status": status,
	})
}

func (h *WorkflowHandler) submitCase(w http.ResponseWriter, r *http.Request, id string) {
	var req caseActorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	// The submit actor (used for the timeline and the authoritative
	// actorUserId/createdBy process variables) is the verified caller, not a
	// client claim; only a superadmin may submit on behalf of another actor.
	actor, err := authenticatedActorForCase(r, req.Actor)
	if err != nil {
		writeCaseActorError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Actor) != "" && strings.TrimSpace(req.Actor) != actor {
		slog.Warn("workflow case submit actor overridden by verified identity",
			"caseId", id, "requestedActor", req.Actor, "actor", actor)
	}

	updated, err := h.workflowCmd.SubmitCase(r.Context(), id, service.SubmitCaseInput{
		Actor:          actor,
		Variables:      req.Variables,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, r, http.StatusNotFound, "Case not found")
			return
		}
		if errors.Is(err, repository.ErrIdempotencyConflict) {
			writeAPIError(w, r, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, service.ErrCaseTypeUnavailable) {
			writeCaseTypeUnavailable(w, r, err)
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

	// assigned_to always comes from the verified caller. The legacy body actor
	// is still accepted for wire compatibility but never trusted.
	actor := currentUserID(r)
	if actor == "" {
		writeCaseActorError(w, r, errCaseActorRequired)
		return
	}
	if requested := strings.TrimSpace(req.Actor); requested != "" && requested != actor {
		slog.Warn("workflow case claim actor ignored in favor of verified identity",
			"caseId", id, "requestedActor", requested, "actor", actor)
	}

	bc, err := h.caseRepo.ClaimCase(r.Context(), id, actor)
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

// parseDate parses filter bounds: RFC3339 keeps the client's explicit
// offset; a date-only value resolves as midnight in the request's business
// timezone (user tz via X-User-Timezone, default Asia/Ho_Chi_Minh) per
// docs/db-schema-conventions.md §8.
func parseDate(loc *time.Location, values ...string) *time.Time {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return &parsed
		}
		local, err := ardatime.ParseDayIn(value, loc)
		if err == nil {
			return &local
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

var (
	errCaseActorRequired = errors.New("verified actor is required")
	errCaseActorMismatch = errors.New("actor does not match the authenticated user")
)

// authenticatedActorForCase resolves the actor of a case command to the
// verified X-User-Id. A body-supplied actor is honored only when it matches the
// verified identity or when the caller is a superadmin (explicit, audited
// impersonation); otherwise the command is rejected so created_by/assigned_to
// cannot be forged.
func authenticatedActorForCase(r *http.Request, bodyActor string) (string, error) {
	actor := currentUserID(r)
	if actor == "" {
		return "", errCaseActorRequired
	}
	bodyActor = strings.TrimSpace(bodyActor)
	if bodyActor == "" || bodyActor == actor {
		return actor, nil
	}
	if isSuperadminActor(r) {
		return bodyActor, nil
	}
	return "", errCaseActorMismatch
}

func writeCaseActorError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, errCaseActorRequired):
		status = http.StatusUnauthorized
	case errors.Is(err, errCaseActorMismatch):
		status = http.StatusForbidden
	}
	writeAPIError(w, r, status, err.Error())
}

// errWorkflowTenantOutsideScope is returned when a workflow management command
// targets a tenant other than the one verified by the BFF context.
var errWorkflowTenantOutsideScope = errors.New("tenant is outside verified scope")

// requiredWorkflowTargetTenant resolves the tenant a workflow management
// command may act on. The verified tenant bound by ardametadata.HTTPMiddleware
// (the same source the repository's verifiedTenant gate uses) is the only
// authority: a tenant_id/tenantId query param may restate it, but a different
// tenant is rejected unless the caller is a superadmin. Missing verified scope
// is rejected as well — never fall back to the query param.
func requiredWorkflowTargetTenant(r *http.Request) (string, error) {
	if r == nil {
		return "", errWorkflowTenantOutsideScope
	}
	verified := strings.TrimSpace(ardametadata.FromOutgoing(r.Context()).TenantID)
	if verified == "" {
		// gRPC-originated contexts carry the verified tenant as incoming
		// metadata; HTTP requests always bind it as outgoing metadata.
		verified = strings.TrimSpace(ardametadata.FromIncoming(r.Context()).TenantID)
	}
	if verified == "" {
		return "", errWorkflowTenantOutsideScope
	}
	requested := ""
	if r.URL != nil {
		requested = strings.TrimSpace(firstString(r.URL.Query().Get("tenant_id"), r.URL.Query().Get("tenantId")))
	}
	if requested == "" || requested == verified {
		return verified, nil
	}
	if !isSuperadminActor(r) {
		return "", errWorkflowTenantOutsideScope
	}
	return requested, nil
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
	return hasToken(currentUserPermissions(r), "superadmin") ||
		hasToken(currentUserRoles(r), "SUPER_ADMIN") ||
		hasToken(splitHeaderTokens(r.Header.Get("X-Global-Permissions")), "superadmin") ||
		hasToken(splitHeaderTokens(r.Header.Get("X-Global-Roles")), "SUPER_ADMIN") ||
		strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Global-Admin")), "true")
}

func (h *WorkflowHandler) canClaimCandidateRole(ctx context.Context, r *http.Request, tenantID, candidateRole string) (bool, error) {
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
	return h.caseRepo.UserCanClaimRole(ctx, tenantID, userID, currentUserGroups(r), candidateRole)
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
	// v2 native user tasks (internal/bootstrap/*.bpmn). Maker steps
	// (UT_MakerInput, UT_MakerRevise) are intentionally absent.
	"UT_CheckerReview": {},
	"UT_GDReview":      {},
	"UT_PGDReview":     {},
	"UT_BoardReview":   {},
	"UT_TWRevalidate":  {},
	// legacy parked v1 ids kept for in-flight processes
	"Activity_CheckerReview": {},
	"Activity_RiskReview":    {},
}

func (h *WorkflowHandler) enforceMakerChecker(r *http.Request, processInstanceKey int64, elementID, actor string) error {
	if processInstanceKey <= 0 || strings.TrimSpace(elementID) == "" || actor == "" {
		return errors.New("task scope could not be verified")
	}
	if _, ok := checkerTaskSteps[strings.TrimSpace(elementID)]; !ok {
		return nil
	}
	if isSuperadminActor(r) || makerCheckerSODRelaxed() {
		return nil
	}
	if h.caseRepo == nil {
		return errors.New("workflow registry is unavailable")
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), processInstanceKey)
	if err != nil || bc == nil {
		// Fail closed: an unresolvable case must not skip the segregation of duties.
		return errors.New("hồ sơ không tồn tại hoặc không thuộc phạm vi của bạn")
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
	writeListAny(w, r, items)
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
