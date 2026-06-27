package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
)

type CustomerHandler struct {
	customerRepo       *repository.CustomerRepository
	workflowServiceURL string
}

func NewCustomerHandler(customerRepo *repository.CustomerRepository, workflowServiceURL string) *CustomerHandler {
	return &CustomerHandler{
		customerRepo:       customerRepo,
		workflowServiceURL: workflowServiceURL,
	}
}

type CreateCustomerRequest struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (h *CustomerHandler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.Name == "" || req.Email == "" {
		http.Error(w, "id, name, and email are required", http.StatusBadRequest)
		return
	}

	// 1. Create in DB with initial status "SUBMITTED"
	err := h.customerRepo.Create(r.Context(), req.ID, req.Name, req.Email, "SUBMITTED")
	if err != nil {
		http.Error(w, "Failed to store customer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Invoke workflow-service to start workflow
	wfReqBody, err := json.Marshal(map[string]any{
		"bpmnProcessId": "customer-onboarding",
		"businessKey":   req.ID,
		"variables": map[string]any{
			"customerId":    req.ID,
			"customerName":  req.Name,
			"customerEmail": req.Email,
		},
	})
	if err != nil {
		http.Error(w, "Failed to build workflow request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/v1/workflows/start", h.workflowServiceURL), "application/json", bytes.NewBuffer(wfReqBody))
	if err != nil {
		http.Error(w, "Failed to call workflow service: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		http.Error(w, fmt.Sprintf("Workflow service returned status %d", resp.StatusCode), http.StatusInternalServerError)
		return
	}

	var startResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		http.Error(w, "Failed to decode workflow response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"customerId": req.ID,
		"status":     "SUBMITTED",
		"workflow":   startResp,
	})
}
