package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WorkflowHandler struct {
	zeebeSvc          *service.ZeebeService
	mappingRepo       *repository.MappingRepository
	caseRepo          *repository.CaseRepository
	processDefinition *repository.ProcessDefinitionRepository
}

func NewWorkflowHandler(zeebeSvc *service.ZeebeService, mappingRepo *repository.MappingRepository, caseRepo *repository.CaseRepository, processDefinition *repository.ProcessDefinitionRepository) *WorkflowHandler {
	return &WorkflowHandler{
		zeebeSvc:          zeebeSvc,
		mappingRepo:       mappingRepo,
		caseRepo:          caseRepo,
		processDefinition: processDefinition,
	}
}

func (h *WorkflowHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(10 << 20) // 10MB
	if err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	key, err := h.zeebeSvc.DeployWorkflow(r.Context(), header.Filename, content)
	if err != nil {
		writeDeployError(w, err)
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
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.BpmnProcessID == "" {
		http.Error(w, "bpmnProcessId is required", http.StatusBadRequest)
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
		http.Error(w, "Failed to start workflow: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if req.BusinessKey != "" {
		err = h.mappingRepo.SaveMapping(r.Context(), req.BusinessKey, key, req.BpmnProcessID, "ACTIVE")
		if err != nil {
			// Log error but proceed
			http.Error(w, "Workflow started but failed to save mapping: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.MessageName == "" || req.CorrelationKey == "" {
		http.Error(w, "messageName and correlationKey are required", http.StatusBadRequest)
		return
	}

	_, err := h.zeebeSvc.PublishMessage(r.Context(), req.MessageName, req.CorrelationKey, req.MessageID, req.Variables)
	if err != nil {
		http.Error(w, "Failed to publish message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "published",
	})
}

func (h *WorkflowHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	instanceKeyStr := parts[len(parts)-2]
	instanceKey, err := strconv.ParseInt(instanceKeyStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid instance key: "+instanceKeyStr, http.StatusBadRequest)
		return
	}

	err = h.zeebeSvc.CancelWorkflow(r.Context(), instanceKey)
	if err != nil {
		http.Error(w, "Failed to cancel workflow: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	businessKey := parts[len(parts)-1]
	mapping, err := h.mappingRepo.GetMapping(r.Context(), businessKey)
	if err != nil {
		http.Error(w, "Failed to query mapping: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if mapping == nil {
		http.Error(w, "Mapping not found", http.StatusNotFound)
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
			http.Error(w, "Failed to query case types: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, caseTypes)
	case http.MethodPost:
		var req repository.CaseTypeUpsert
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateCaseType(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) CaseTypeByID(w http.ResponseWriter, r *http.Request) {
	caseType, action := caseTypePath(r.URL.Path)
	if caseType == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut || (action != "" && action != "process-config") {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if action == "process-config" {
		var req repository.ProcessConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.UpdateProcessConfig(r.Context(), caseType, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if item == nil {
			http.Error(w, "Case type not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}

	var req repository.CaseTypeUpsert
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateCaseType(r.Context(), caseType, req)
	writeUpdateOrError(w, item, err, "Case type not found")
}

func (h *WorkflowHandler) SLAPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListSLAPolicies(r.Context())
		writeListOrError(w, "slaPolicies", items, err)
	case http.MethodPost:
		var req repository.SLAPolicy
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateSLAPolicy(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) SLAPolicyByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/sla-policies/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req repository.SLAPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateSLAPolicy(r.Context(), id, req)
	writeUpdateOrError(w, item, err, "SLA policy not found")
}

func (h *WorkflowHandler) DescriptionTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListDescriptionTemplates(r.Context())
		writeListOrError(w, "descriptionTemplates", items, err)
	case http.MethodPost:
		var req repository.DescriptionTemplate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateDescriptionTemplate(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) DescriptionTemplateByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/description-templates/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req repository.DescriptionTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateDescriptionTemplate(r.Context(), id, req)
	writeUpdateOrError(w, item, err, "Description template not found")
}

func (h *WorkflowHandler) ProcessRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListProcessRoles(r.Context())
		writeListOrError(w, "processRoles", items, err)
	case http.MethodPost:
		var req repository.ProcessRole
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateProcessRole(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) ProcessDefinitions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.processDefinition.List(r.Context())
		writeListOrError(w, "processDefinitions", items, err)
	case http.MethodPost:
		in, err := parseProcessDefinitionImport(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.processDefinition.Create(r.Context(), in)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.processDefinition.Update(r.Context(), id, in)
		writeUpdateOrError(w, item, err, "Process definition not found")
	case r.Method == http.MethodGet && action == "xml":
		item, err := h.processDefinition.Get(r.Context(), id)
		if err != nil {
			http.Error(w, "Failed to query process definition: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if item == nil {
			http.Error(w, "Process definition not found", http.StatusNotFound)
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
		http.Error(w, "Zeebe service is not configured", http.StatusServiceUnavailable)
		return
	}
	item, err := h.processDefinition.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query process definition: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if item == nil {
		http.Error(w, "Process definition not found", http.StatusNotFound)
		return
	}

	key, err := h.zeebeSvc.DeployWorkflow(r.Context(), item.ResourceName, []byte(item.XMLContent))
	if err != nil {
		writeDeployError(w, err)
		return
	}
	deployed, err := h.processDefinition.MarkDeployed(r.Context(), id, key)
	writeUpdateOrError(w, deployed, err, "Process definition not found")
}

func (h *WorkflowHandler) ProcessRoleByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/roles/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req repository.ProcessRole
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateProcessRole(r.Context(), id, req)
	writeUpdateOrError(w, item, err, "Process role not found")
}

func (h *WorkflowHandler) RoleCatalog(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowRoleCatalog(r.Context())
		writeListOrError(w, "roleCatalog", items, err)
	case http.MethodPost:
		var req repository.WorkflowRoleCatalog
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateWorkflowRoleCatalog(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) RoleCatalogByCode(w http.ResponseWriter, r *http.Request) {
	roleCode := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/role-catalog/"), "/")
	if roleCode == "" || strings.Contains(roleCode, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req repository.WorkflowRoleCatalog
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateWorkflowRoleCatalog(r.Context(), roleCode, req)
	writeUpdateOrError(w, item, err, "Workflow role not found")
}

func (h *WorkflowHandler) RoleMemberships(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowRoleMemberships(r.Context())
		writeListOrError(w, "roleMemberships", items, err)
	case http.MethodPost:
		var req repository.WorkflowRoleMembership
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateWorkflowRoleMembership(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) RoleMembershipByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/role-memberships/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req repository.WorkflowRoleMembership
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateWorkflowRoleMembership(r.Context(), id, req)
	writeUpdateOrError(w, item, err, "Workflow role membership not found")
}

func (h *WorkflowHandler) AssignmentRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowAssignmentRules(r.Context())
		writeListOrError(w, "assignmentRules", items, err)
	case http.MethodPost:
		var req repository.WorkflowAssignmentRule
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateWorkflowAssignmentRule(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) AssignmentRuleByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/assignment-rules/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req repository.WorkflowAssignmentRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateWorkflowAssignmentRule(r.Context(), id, req)
	writeUpdateOrError(w, item, err, "Workflow assignment rule not found")
}

func (h *WorkflowHandler) Delegations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.caseRepo.ListWorkflowDelegations(r.Context())
		writeListOrError(w, "delegations", items, err)
	case http.MethodPost:
		var req repository.WorkflowDelegation
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		item, err := h.caseRepo.CreateWorkflowDelegation(r.Context(), req)
		writeMutationOrError(w, item, err)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandler) DelegationByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workflow/delegations/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req repository.WorkflowDelegation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.caseRepo.UpdateWorkflowDelegation(r.Context(), id, req)
	writeUpdateOrError(w, item, err, "Workflow delegation not found")
}

func (h *WorkflowHandler) Cases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listCases(w, r)
	case http.MethodPost:
		h.createCase(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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
		http.Error(w, "Failed to query cases: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, cases)
}

func (h *WorkflowHandler) createCase(w http.ResponseWriter, r *http.Request) {
	var req repository.CaseCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	bc, err := h.caseRepo.CreateCase(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, bc)
}

func (h *WorkflowHandler) getCase(w http.ResponseWriter, r *http.Request, id string) {
	bc, err := h.caseRepo.GetCase(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query case: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if bc == nil {
		http.Error(w, "Case not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, bc)
}

func (h *WorkflowHandler) caseTimeline(w http.ResponseWriter, r *http.Request, id string) {
	events, err := h.caseRepo.ListTimeline(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query timeline: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

type caseActorRequest struct {
	Actor string `json:"actor"`
}

func (h *WorkflowHandler) submitCase(w http.ResponseWriter, r *http.Request, id string) {
	var req caseActorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	bc, err := h.caseRepo.GetCase(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query case: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if bc == nil {
		http.Error(w, "Case not found", http.StatusNotFound)
		return
	}
	if bc.BpmnProcessID == nil {
		http.Error(w, "Case has no BPMN process configured", http.StatusBadRequest)
		return
	}
	if bc.Status != repository.CaseStatusDraft {
		http.Error(w, "Case status must be DRAFT", http.StatusBadRequest)
		return
	}
	if h.zeebeSvc == nil {
		http.Error(w, "Zeebe service is not configured", http.StatusServiceUnavailable)
		return
	}
	if req.Actor == "" {
		req.Actor = bc.CreatedBy
	}

	variables := map[string]any{
		"caseId":            bc.ID,
		"caseType":          bc.CaseType,
		"caseCode":          bc.CaseCode,
		"tenantId":          bc.TenantID,
		"domainService":     bc.DomainService,
		"primaryObjectType": bc.PrimaryObjectType,
		"primaryObjectId":   bc.PrimaryObjectID,
	}
	processKey, err := h.zeebeSvc.StartWorkflow(r.Context(), *bc.BpmnProcessID, variables)
	if err != nil {
		http.Error(w, "Failed to start workflow: "+err.Error(), http.StatusInternalServerError)
		return
	}

	updated, err := h.caseRepo.SubmitCase(r.Context(), id, req.Actor, processKey)
	if err != nil {
		http.Error(w, "Workflow started but case submit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *WorkflowHandler) claimCase(w http.ResponseWriter, r *http.Request, id string) {
	var req caseActorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	bc, err := h.caseRepo.ClaimCase(r.Context(), id, req.Actor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if bc == nil {
		http.Error(w, "Case not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, bc)
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

func writeListOrError(w http.ResponseWriter, key string, items any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{key: items})
}

func writeMutationOrError(w http.ResponseWriter, item any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func writeUpdateOrError(w http.ResponseWriter, item any, err error, notFound string) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if item == nil {
		http.Error(w, notFound, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeDeployError(w http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	if status.Code(err) == codes.InvalidArgument {
		statusCode = http.StatusBadRequest
	}
	http.Error(w, "Failed to deploy: "+err.Error(), statusCode)
}
