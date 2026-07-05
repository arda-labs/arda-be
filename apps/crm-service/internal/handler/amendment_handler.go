package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
)

type AmendmentHandler struct {
	customerRepo   *repository.CustomerRepository
	amendmentRepo  *repository.AmendmentRepository
	workflowClient *workflowclient.Client
}

func NewAmendmentHandler(
	customerRepo *repository.CustomerRepository,
	amendmentRepo *repository.AmendmentRepository,
	workflowClient *workflowclient.Client,
) *AmendmentHandler {
	return &AmendmentHandler{
		customerRepo:   customerRepo,
		amendmentRepo:  amendmentRepo,
		workflowClient: workflowClient,
	}
}

func (h *AmendmentHandler) Route(w http.ResponseWriter, r *http.Request) {
	customerID, segments := customerSegments(r.URL.Path)
	if customerID == "" || len(segments) < 1 || segments[0] != "adjustments" {
		http.NotFound(w, r)
		return
	}
	if len(segments) == 1 {
		switch r.Method {
		case http.MethodPost:
			h.startAdjustment(w, r, customerID)
		case http.MethodGet:
			h.getCurrentAmendment(w, r, customerID)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if len(segments) >= 2 {
		h.CustomerAmendmentByID(w, r)
		return
	}
	http.NotFound(w, r)
}

func (h *AmendmentHandler) CustomerAmendmentByID(w http.ResponseWriter, r *http.Request) {
	customerID, segments := customerSegments(r.URL.Path)
	if customerID == "" || len(segments) < 2 || segments[0] != "adjustments" {
		http.NotFound(w, r)
		return
	}
	amendmentID := segments[1]
	action := ""
	if len(segments) == 3 {
		action = segments[2]
	}

	switch {
	case r.Method == http.MethodPut && action == "":
		h.updateAmendment(w, r, customerID, amendmentID)
	case r.Method == http.MethodPost && action == "submit":
		h.submitAmendment(w, r, customerID, amendmentID)
	case r.Method == http.MethodPost && action == "cancel":
		h.cancelAmendment(w, r, customerID, amendmentID)
	default:
		http.NotFound(w, r)
	}
}

func (h *AmendmentHandler) startAdjustment(w http.ResponseWriter, r *http.Request, customerID string) {
	customer, err := h.customerInScope(r, customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if customer.Status != "ACTIVE" {
		http.Error(w, "Customer must be ACTIVE to start adjustment", http.StatusConflict)
		return
	}
	pending, err := h.amendmentRepo.HasPending(r.Context(), customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pending {
		http.Error(w, "Customer already has a pending amendment", http.StatusConflict)
		return
	}

	caseID, err := h.createAdjustmentCase(r, customer)
	if err != nil {
		http.Error(w, "Failed to create workflow case: "+err.Error(), http.StatusBadGateway)
		return
	}
	amendment, err := h.amendmentRepo.CreateDraft(r.Context(), customerID, caseID)
	if err != nil {
		http.Error(w, "Failed to create amendment: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, amendment)
}

func (h *AmendmentHandler) getCurrentAmendment(w http.ResponseWriter, r *http.Request, customerID string) {
	if _, err := h.customerInScope(r, customerID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	amendment, err := h.amendmentRepo.GetPendingByCustomer(r.Context(), customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, amendment)
}

func (h *AmendmentHandler) updateAmendment(w http.ResponseWriter, r *http.Request, customerID, amendmentID string) {
	if _, err := h.customerInScope(r, customerID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	amendment, err := h.amendmentRepo.Get(r.Context(), amendmentID)
	if err != nil || amendment == nil || amendment.CustomerID != customerID {
		http.Error(w, "Amendment not found", http.StatusNotFound)
		return
	}
	var req repository.AmendmentUpsert
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	updated, err := h.amendmentRepo.UpdateDraft(r.Context(), amendmentID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *AmendmentHandler) submitAmendment(w http.ResponseWriter, r *http.Request, customerID, amendmentID string) {
	customer, err := h.customerInScope(r, customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	amendment, err := h.amendmentRepo.Get(r.Context(), amendmentID)
	if err != nil || amendment == nil || amendment.CustomerID != customerID {
		http.Error(w, "Amendment not found", http.StatusNotFound)
		return
	}
	if amendment.WorkflowCaseID == "" {
		http.Error(w, "Amendment has no workflow case", http.StatusBadRequest)
		return
	}
	actor := actorFromRequest(r)
	if err := h.submitAdjustmentCase(r, amendment.WorkflowCaseID, customer, actor); err != nil {
		http.Error(w, "Failed to submit workflow case: "+err.Error(), http.StatusBadGateway)
		return
	}
	updated, err := h.amendmentRepo.Submit(r.Context(), amendmentID, actor, repository.CustomerSnapshot(customer))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *AmendmentHandler) cancelAmendment(w http.ResponseWriter, r *http.Request, customerID, amendmentID string) {
	if _, err := h.customerInScope(r, customerID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	amendment, err := h.amendmentRepo.Get(r.Context(), amendmentID)
	if err != nil || amendment == nil || amendment.CustomerID != customerID {
		http.Error(w, "Amendment not found", http.StatusNotFound)
		return
	}
	if amendment.Status != "DRAFT" {
		http.Error(w, "Only draft amendments can be cancelled", http.StatusConflict)
		return
	}
	if err := h.amendmentRepo.CancelDraft(r.Context(), amendmentID, customerID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

func (h *AmendmentHandler) createAdjustmentCase(r *http.Request, customer *repository.Customer) (string, error) {
	if h.workflowClient == nil {
		return "", fmt.Errorf("workflow client is not configured")
	}
	actor := actorFromRequest(r)
	createdCase, err := h.workflowClient.CreateCase(r.Context(), workflowclient.CaseCreate{
		TenantID:          customer.TenantID,
		CaseType:          "CUSTOMER_ADJUSTMENT",
		CaseCode:          customer.CustomerCode,
		Title:             fmt.Sprintf("Điều chỉnh KH %s - %s", customer.CustomerCode, customer.Name),
		PrimaryObjectType: "CUSTOMER",
		PrimaryObjectID:   customer.ID,
		DomainService:     "crm-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
	})
	if err != nil {
		return "", err
	}
	return createdCase.GetId(), nil
}

func (h *AmendmentHandler) submitAdjustmentCase(r *http.Request, caseID string, customer *repository.Customer, actor string) error {
	_, err := h.workflowClient.SubmitCase(r.Context(), caseID, actor, map[string]any{
		"customerId":   customer.ID,
		"customerCode": customer.CustomerCode,
		"customerName": customer.Name,
		"adjustment":   true,
	})
	return err
}

func customerSegments(path string) (string, []string) {
	const prefix = "/api/crm/customers/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return "", nil
	}
	parts := strings.Split(rest, "/")
	return parts[0], parts[1:]
}

func actorFromRequest(r *http.Request) string {
	actor := r.Header.Get("X-User-Id")
	if actor == "" {
		actor = "crm-maker"
	}
	return actor
}

func (h *AmendmentHandler) customerInScope(r *http.Request, customerID string) (*repository.Customer, error) {
	scope := ScopeFromRequest(r)
	customer, err := h.customerRepo.Get(r.Context(), customerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query customer: %w", err)
	}
	if customer == nil || !scope.AllowsOrg(customer.OrgID) {
		return nil, fmt.Errorf("customer not found")
	}
	return customer, nil
}
