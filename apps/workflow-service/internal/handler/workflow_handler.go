package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

type WorkflowHandler struct {
	zeebeSvc    *service.ZeebeService
	mappingRepo *repository.MappingRepository
}

func NewWorkflowHandler(zeebeSvc *service.ZeebeService, mappingRepo *repository.MappingRepository) *WorkflowHandler {
	return &WorkflowHandler{
		zeebeSvc:    zeebeSvc,
		mappingRepo: mappingRepo,
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
		http.Error(w, "Failed to deploy: "+err.Error(), http.StatusInternalServerError)
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
