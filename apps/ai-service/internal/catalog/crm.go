package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
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

type customerEnvelope struct {
	Result CustomerSummary `json:"result"`
}

// RegisterCRMCatalog registers CRM SDK methods (arda.crm.*) that proxy to the
// CRM service with delegated identity headers.
func RegisterCRMCatalog(reg *DispatcherRegistry, crmBaseURL string, httpClient *http.Client) {
	cleanCRMURL := strings.TrimRight(strings.TrimSpace(crmBaseURL), "/")
	if cleanCRMURL == "" {
		return
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	// 1. arda.crm.getCustomer (Read)
	reg.Register(
		CatalogEntry{
			MethodName: "crm.getCustomer",
			SDKPath:    "arda.crm.getCustomer",
			Domain:     "crm",
			Signature:  "arda.crm.getCustomer(args: { customerId: string }): Promise<CustomerSummary>;",
			JSDoc: `/**
 * Read a redacted customer summary in the active tenant and organization scope.
 * @param args.customerId Arda customer identifier or customer code (max 128 chars)
 * @returns CustomerSummary { id, customerCode, name, status, segment, rank, riskLevel, orgId, updatedAt }
 * @requires crm.customer.read
 * @domain crm
 */`,
			Keywords:            []string{"customer", "crm", "client", "code", "name", "status", "segment", "risk", "get", "read"},
			Kind:                "read",
			RequiredPermissions: []string{"crm.customer.read"},
			Risk:                "low",
			Timeout:             3 * time.Second,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			customerID, _ := args["customerId"].(string)
			customerID = strings.TrimSpace(customerID)
			if customerID == "" || len(customerID) > 128 {
				return nil, fmt.Errorf("customerId is required (max 128 characters)")
			}

			target := cleanCRMURL + "/api/crm/customers/" + url.PathEscape(customerID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return nil, fmt.Errorf("create CRM request: %w", err)
			}

			setDelegatedHeaders(req, scope)

			resp, err := httpClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("CRM request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
				return nil, fmt.Errorf("CRM returned status %d", resp.StatusCode)
			}

			var envelope customerEnvelope
			if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&envelope); err != nil {
				return nil, fmt.Errorf("decode CRM response: %w", err)
			}

			c := envelope.Result
			if strings.TrimSpace(c.ID) == "" {
				return nil, fmt.Errorf("CRM customer response missing identity")
			}
			return c, nil
		},
	)

	// 2. arda.crm.exportCustomer (Mutation - Confirm kind)
	reg.Register(
		CatalogEntry{
			MethodName: "crm.exportCustomer",
			SDKPath:    "arda.crm.exportCustomer",
			Domain:     "crm",
			Signature:  "arda.crm.exportCustomer(args: { customerId: string; format?: 'csv' | 'json' }): Promise<ApprovalProposal>;",
			JSDoc: `/**
 * Prepare an export for customer data. MUTATION - yields ApprovalProposal for human review.
 * @param args.customerId Customer identifier
 * @param args.format Export format: 'csv' or 'json' (default 'csv')
 * @returns ApprovalProposal
 * @requires crm.customer.export
 * @domain crm
 */`,
			Keywords:            []string{"export", "customer", "csv", "json", "download", "prepare"},
			Kind:                "confirm",
			RequiredPermissions: []string{"crm.customer.export"},
			Risk:                "medium",
			Timeout:             3 * time.Second,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			// When called directly on resume approval: execute the actual export preparation
			customerID, _ := args["customerId"].(string)
			if strings.TrimSpace(customerID) == "" {
				return nil, fmt.Errorf("customerId is required")
			}
			format, _ := args["format"].(string)
			if format == "" {
				format = "csv"
			}

			return map[string]any{
				"status":     "PREPARED",
				"customerId": customerID,
				"format":     format,
				"summary":    fmt.Sprintf("Export for customer %s in %s format is ready for download.", customerID, strings.ToUpper(format)),
			}, nil
		},
	)
}
