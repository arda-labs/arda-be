package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type CustomerSummary struct {
	ID           string    `json:"id"`
	CustomerCode string    `json:"customerCode"`
	Name         string    `json:"name"`
	CustomerType string    `json:"customerType"`
	Status       string    `json:"status"`
	Segment      string    `json:"segment,omitempty"`
	Rank         string    `json:"rank,omitempty"`
	RiskLevel    string    `json:"riskLevel,omitempty"`
	OrgID        string    `json:"orgId"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type customerResponse struct {
	ID           string    `json:"id"`
	CustomerCode string    `json:"customerCode"`
	Name         string    `json:"name"`
	CustomerType string    `json:"customerType"`
	Status       string    `json:"status"`
	Segment      string    `json:"segment"`
	Rank         string    `json:"rank"`
	RiskLevel    string    `json:"riskLevel"`
	OrgID        string    `json:"orgId"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type customerEnvelope struct {
	Result customerResponse `json:"result"`
}

type customerGetArguments struct {
	CustomerID string `json:"customerId"`
}

type CRMCustomerGetTool struct {
	baseURL string
	client  *http.Client
}

func NewCRMCustomerGetTool(baseURL string, client *http.Client) *CRMCustomerGetTool {
	if client == nil {
		client = &http.Client{}
	}
	return &CRMCustomerGetTool{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), client: client}
}

func (t *CRMCustomerGetTool) Definition() Definition {
	return Definition{
		Name:                "crm.customer.get",
		Version:             1,
		Kind:                "read",
		Description:         "Read a redacted customer summary in the active tenant and organization scope",
		RequiredPermissions: []string{"crm.customer.read"},
		Risk:                "low",
		Timeout:             3 * time.Second,
		RedactionProfile:    "customer_summary",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"customerId": {"type": "string", "maxLength": 128, "description": "Arda customer identifier or customer code"}
			},
			"required": ["customerId"]
		}`),
	}
}

func (t *CRMCustomerGetTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	var input customerGetArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.CustomerID) == "" || len(input.CustomerID) > 128 {
		return Result{}, fmt.Errorf("%w: customerId is required", ErrInvalidArgument)
	}
	if t == nil || t.baseURL == "" {
		return Result{}, fmt.Errorf("CRM service is not configured")
	}

	toolContext, cancel := context.WithTimeout(ctx, t.Definition().Timeout)
	defer cancel()
	target := t.baseURL + "/api/crm/customers/" + url.PathEscape(input.CustomerID)
	req, err := http.NewRequestWithContext(toolContext, http.MethodGet, target, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create CRM request: %w", err)
	}
	// These values come from the gateway-verified request and are never taken
	// from tool arguments. CRM applies the same tenant/org scope to the read.
	req.Header.Set("X-Tenant-Id", scope.TenantID)
	req.Header.Set("X-User-Id", scope.ActorUserID)
	req.Header.Set("X-User-Org-Ids", strings.Join(scope.OrgIDs, ","))
	if scope.ActiveOrgID != "" {
		req.Header.Set("X-Org-Id", scope.ActiveOrgID)
	}
	req.Header.Set("X-Auth-Checked", "true")
	if scope.RequestID != "" {
		req.Header.Set("X-Request-Id", scope.RequestID)
	}

	response, err := t.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("CRM customer read failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return Result{}, fmt.Errorf("CRM customer read returned status %d", response.StatusCode)
	}
	var envelope customerEnvelope
	decoder = json.NewDecoder(io.LimitReader(response.Body, 256<<10))
	if err := decoder.Decode(&envelope); err != nil {
		return Result{}, fmt.Errorf("decode CRM customer response: %w", err)
	}
	customer := envelope.Result
	if strings.TrimSpace(customer.ID) == "" || strings.TrimSpace(customer.CustomerCode) == "" {
		return Result{}, fmt.Errorf("CRM customer response is missing its stable identity")
	}

	summary := CustomerSummary{
		ID: customer.ID, CustomerCode: customer.CustomerCode, Name: customer.Name,
		CustomerType: customer.CustomerType, Status: customer.Status, Segment: customer.Segment,
		Rank: customer.Rank, RiskLevel: customer.RiskLevel, OrgID: customer.OrgID, UpdatedAt: customer.UpdatedAt,
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return Result{}, fmt.Errorf("encode CRM customer summary: %w", err)
	}
	return Result{
		Data: data, Summary: fmt.Sprintf("Customer %s (%s) is %s.", summary.Name, summary.CustomerCode, summary.Status),
		Source: "crm-service", RequestID: scope.RequestID, FreshAt: time.Now().UTC(),
	}, nil
}
