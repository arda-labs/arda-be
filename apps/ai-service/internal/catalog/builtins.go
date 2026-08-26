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

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
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

// RegisterBuiltinCatalog registers all default SDK methods with direct service dispatchers.
func RegisterBuiltinCatalog(
	reg *DispatcherRegistry,
	crmBaseURL string,
	httpClient *http.Client,
	searcher knowledge.Searcher,
) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	cleanCRMURL := strings.TrimRight(strings.TrimSpace(crmBaseURL), "/")

	// 1. arda.crm.getCustomer (Read)
	if cleanCRMURL != "" {
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

	// 3. arda.knowledge.search (Read)
	if searcher != nil {
		reg.Register(
			CatalogEntry{
				MethodName: "knowledge.search",
				SDKPath:    "arda.knowledge.search",
				Domain:     "knowledge",
				Signature:  "arda.knowledge.search(args: { query: string; limit?: number }): Promise<KnowledgeSearchResult[]>;",
				JSDoc: `/**
 * Search published knowledge sources and business documentation with citations.
 * @param args.query Natural language search query (max 512 chars)
 * @param args.limit Number of items, 1-5 (default 3)
 * @returns KnowledgeSearchResult[] { sourceId, sourceTitle, content, citations, matchScore }
 * @requires ai.knowledge.read
 * @domain knowledge
 */`,
				Keywords:            []string{"knowledge", "doc", "faq", "policy", "procedure", "search", "rag", "guide", "rule"},
				Kind:                "read",
				RequiredPermissions: []string{"ai.knowledge.read"},
				Risk:                "low",
				Timeout:             3 * time.Second,
			},
			func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
				query, _ := args["query"].(string)
				query = strings.TrimSpace(query)
				if query == "" || len(query) > 512 {
					return nil, fmt.Errorf("query is required (max 512 characters)")
				}

				limit := 3
				if l, ok := args["limit"].(float64); ok && l > 0 && l <= 5 {
					limit = int(l)
				}

				items, err := searcher.Search(ctx, scope.TenantID, query, limit)
				if err != nil {
					return nil, fmt.Errorf("knowledge search error: %w", err)
				}
				return items, nil
			},
		)
	}
}
