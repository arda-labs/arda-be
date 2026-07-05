package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
)

type CustomerHandler struct {
	customerRepo   *repository.CustomerRepository
	workflowClient *workflowclient.Client
}

func NewCustomerHandler(customerRepo *repository.CustomerRepository, workflowClient *workflowclient.Client) *CustomerHandler {
	return &CustomerHandler{
		customerRepo:   customerRepo,
		workflowClient: workflowClient,
	}
}

func (h *CustomerHandler) Customers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listCustomers(w, r)
	case http.MethodPost:
		h.saveCustomer(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *CustomerHandler) CustomerByID(w http.ResponseWriter, r *http.Request) {
	id, action := customerPath(r.URL.Path)
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		h.getCustomer(w, r, id)
	case r.Method == http.MethodPut && action == "":
		h.saveCustomer(w, r)
	case r.Method == http.MethodPost && action == "submit":
		h.submitCustomer(w, r, id)
	case r.Method == http.MethodPost && action == "cancel":
		h.cancelCustomer(w, r, id)
	case r.Method == http.MethodGet && action == "relationships":
		h.listRelationships(w, r, id)
	case r.Method == http.MethodPost && action == "relationships":
		h.createRelationship(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (h *CustomerHandler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	h.Customers(w, r)
}

func (h *CustomerHandler) listCustomers(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.customerRepo.ListCustomers(r.Context(), repository.CustomerListFilter{
		TenantID:     scope.TenantID,
		OrgIDs:       scope.OrgIDs,
		CustomerType: r.URL.Query().Get("customerType"),
		Status:       r.URL.Query().Get("status"),
		RiskOnly:     r.URL.Query().Get("riskOnly") == "true",
		Q:            r.URL.Query().Get("q"),
		Limit:        limit,
	})
	if err != nil {
		http.Error(w, "Failed to query customers: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *CustomerHandler) getCustomer(w http.ResponseWriter, r *http.Request, id string) {
	scope := ScopeFromRequest(r)
	item, err := h.customerRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query customer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if item == nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}
	if !scope.AllowsOrg(item.OrgID) {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *CustomerHandler) saveCustomer(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	var req repository.CustomerUpsert
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if id, _ := customerPath(r.URL.Path); id != "" {
		req.ID = id
	}
	req.TenantID = scope.TenantID
	if req.ID == "" {
		req.OrgID = scope.ResolveOrgID()
		if req.OrgID == "" && len(scope.OrgIDs) > 0 {
			req.OrgID = scope.OrgIDs[0]
		}
	}
	item, err := h.customerRepo.UpsertCustomer(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *CustomerHandler) submitCustomer(w http.ResponseWriter, r *http.Request, id string) {
	scope := ScopeFromRequest(r)
	item, err := h.customerRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query customer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if item == nil || !scope.AllowsOrg(item.OrgID) {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}
	if item.Status != "DRAFT" && item.Status != "NEEDS_CHANGES" {
		http.Error(w, "Customer status must be DRAFT or NEEDS_CHANGES", http.StatusBadRequest)
		return
	}
	if item.Status == "NEEDS_CHANGES" && item.WorkflowCaseID != "" {
		http.Error(w, "Complete the maker revise task in workflow before resubmitting", http.StatusConflict)
		return
	}
	caseID, err := h.submitWorkflowCase(r, item)
	if err != nil {
		http.Error(w, "Failed to submit workflow case: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := h.customerRepo.UpdateSubmitted(r.Context(), id, caseID); err != nil {
		http.Error(w, "Failed to submit customer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := h.customerRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query customer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *CustomerHandler) cancelCustomer(w http.ResponseWriter, r *http.Request, id string) {
	scope := ScopeFromRequest(r)
	item, err := h.customerRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query customer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if item == nil || !scope.AllowsOrg(item.OrgID) {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}
	if err := h.customerRepo.CancelDraft(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "cannot be cancelled") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "Failed to cancel customer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := h.customerRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query customer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *CustomerHandler) submitWorkflowCase(r *http.Request, item *repository.Customer) (string, error) {
	if h.workflowClient == nil {
		return "", fmt.Errorf("workflow client is not configured")
	}
	actor := r.Header.Get("X-User-Id")
	if actor == "" {
		actor = "crm-maker"
	}
	displayCode := item.CustomerCode
	if displayCode == "" {
		displayCode = item.ID
	}
	createdCase, err := h.workflowClient.CreateCase(r.Context(), workflowclient.CaseCreate{
		TenantID:          item.TenantID,
		CaseType:          "CUSTOMER_REGISTRATION",
		CaseCode:          displayCode,
		Title:             fmt.Sprintf("Đăng ký khách hàng %s - %s", displayCode, item.Name),
		PrimaryObjectType: "CUSTOMER",
		PrimaryObjectID:   item.ID,
		DomainService:     "crm-service",
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
		"customerId":     item.ID,
		"customerCode":   displayCode,
		"customerName":   item.Name,
		"customerEmail":  item.Email,
		"identityNo":     item.IdentityNo,
		"riskLevel":      item.RiskLevel,
		"customerStatus": "SUBMITTED",
	})
	if err != nil {
		return "", err
	}
	return createdCase.GetId(), nil
}

func (h *CustomerHandler) listRelationships(w http.ResponseWriter, r *http.Request, id string) {
	items, err := h.customerRepo.ListRelationships(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to query relationships: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *CustomerHandler) createRelationship(w http.ResponseWriter, r *http.Request, id string) {
	var req repository.CustomerRelationshipCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	item, err := h.customerRepo.CreateRelationship(r.Context(), id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func customerPath(path string) (string, string) {
	const prefix = "/api/crm/customers/"
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
