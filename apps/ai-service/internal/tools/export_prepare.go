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

const exportPrepareToolName = "crm.customer.export.prepare"

type exportPrepareArguments struct {
	CustomerID string `json:"customerId"`
	Format     string `json:"format"`
}

type exportPreparePayload struct {
	Prepared   bool   `json:"prepared"`
	CustomerID string `json:"customerId"`
	Format     string `json:"format"`
	VerifiedAt string `json:"verifiedAt"`
}

type CRMCustomerExportPrepareTool struct {
	baseURL string
	client  *http.Client
}

func NewCRMCustomerExportPrepareTool(baseURL string, client *http.Client) *CRMCustomerExportPrepareTool {
	if client == nil {
		client = &http.Client{}
	}
	return &CRMCustomerExportPrepareTool{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), client: client}
}

func (t *CRMCustomerExportPrepareTool) Definition() Definition {
	return Definition{
		Name:                exportPrepareToolName,
		Version:             1,
		Kind:                "confirm",
		Description:         "Prepare (not execute) a customer data export request. Requires explicit human approval before any run.",
		RequiredPermissions: []string{"crm.customer.read"},
		Risk:                "confirm",
		Timeout:             3 * time.Second,
		RedactionProfile:    "customer_summary",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"customerId": {"type": "string", "maxLength": 128, "description": "Arda customer identifier"},
				"format": {"type": "string", "enum": ["csv", "json"], "description": "Requested export format"}
			},
			"required": ["customerId", "format"]
		}`),
	}
}

// Execute verifies the customer is readable in scope and returns a bounded
// preparation payload. It never creates an export artifact.
func (t *CRMCustomerExportPrepareTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	var input exportPrepareArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return Result{}, fmt.Errorf("%w: invalid export prepare arguments", ErrInvalidArgument)
	}
	input.CustomerID = strings.TrimSpace(input.CustomerID)
	input.Format = strings.ToLower(strings.TrimSpace(input.Format))
	if input.CustomerID == "" || len(input.CustomerID) > 128 || (input.Format != "csv" && input.Format != "json") {
		return Result{}, fmt.Errorf("%w: customerId and format (csv|json) are required", ErrInvalidArgument)
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
	var envelope struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&envelope); err != nil {
		return Result{}, fmt.Errorf("decode CRM customer response: %w", err)
	}
	if strings.TrimSpace(envelope.Result.ID) == "" {
		return Result{}, fmt.Errorf("CRM customer response is missing its stable identity")
	}

	payload := exportPreparePayload{
		Prepared:   true,
		CustomerID: envelope.Result.ID,
		Format:     input.Format,
		VerifiedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("encode export prepare result: %w", err)
	}
	return Result{
		Data: data,
		Summary: fmt.Sprintf("Đã chuẩn bị yêu cầu xuất dữ liệu khách hàng %s (định dạng %s) và chờ phê duyệt.",
			input.CustomerID, input.Format),
		Source: "crm-service", RequestID: scope.RequestID, FreshAt: time.Now().UTC(),
	}, nil
}
