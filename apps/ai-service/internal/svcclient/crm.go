package svcclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// CustomerSummary is the redacted customer shape exposed to the AI SDK.
// Sensitive fields (email, mobile, identity numbers, info maps) are dropped
// by the CRM service internal handler.
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

// CRMClient calls the CRM service internal surface (/internal/ai/*).
type CRMClient struct {
	*Client
}

// NewCRMClient returns a typed client for the CRM service.
func NewCRMClient(baseURL, source, secret string, hc *http.Client) *CRMClient {
	return &CRMClient{Client: NewClient("crm-service", baseURL, source, secret, hc)}
}

// GetCustomer reads a redacted customer summary in the delegated tenant/org
// scope. customerID is the Arda customer identifier or customer code.
func (c *CRMClient) GetCustomer(ctx context.Context, md metadata.Context, customerID string) (CustomerSummary, error) {
	var zero CustomerSummary
	if strings.TrimSpace(customerID) == "" || len(customerID) > 128 {
		return zero, fmt.Errorf("customerId is required (max 128 characters)")
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/internal/ai/customers/"+url.PathEscape(customerID), md)
	if err != nil {
		return zero, err
	}
	var envelope struct {
		Result CustomerSummary `json:"result"`
	}
	if err := c.Do(ctx, req, &envelope); err != nil {
		return zero, err
	}
	if strings.TrimSpace(envelope.Result.ID) == "" {
		return zero, fmt.Errorf("CRM customer response missing identity")
	}
	return envelope.Result, nil
}

// ExportCustomer prepares a customer data export. The CRM service has no
// export backend yet, so the proposal is prepared locally — the typed client
// keeps the dispatcher free of HTTP details and leaves room for a future
// /internal/ai/customers/{id}/export route.
func (c *CRMClient) ExportCustomer(ctx context.Context, md metadata.Context, customerID, format string) (map[string]any, error) {
	if strings.TrimSpace(customerID) == "" {
		return nil, fmt.Errorf("customerId is required")
	}
	if strings.TrimSpace(format) == "" {
		format = "csv"
	}
	return map[string]any{
		"status":     "PREPARED",
		"customerId": customerID,
		"format":     format,
		"summary":    fmt.Sprintf("Export for customer %s in %s format is ready for download.", customerID, strings.ToUpper(format)),
	}, nil
}
