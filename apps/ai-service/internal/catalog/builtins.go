package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// RegisterBuiltinCatalog registers all default SDK methods and binds them to the tools implementations.
func RegisterBuiltinCatalog(
	reg *DispatcherRegistry,
	crmGetTool *tools.CRMCustomerGetTool,
	crmExportTool *tools.CRMCustomerExportPrepareTool,
	knowledgeTool *tools.KnowledgeSearchTool,
) {
	// 1. arda.crm.getCustomer
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
			rawArgs, err := json.Marshal(args)
			if err != nil {
				return nil, fmt.Errorf("invalid arguments: %w", err)
			}
			res, err := crmGetTool.Execute(ctx, scope, rawArgs)
			if err != nil {
				return nil, err
			}
			var parsed any
			if err := json.Unmarshal(res.Data, &parsed); err != nil {
				return string(res.Data), nil
			}
			return parsed, nil
		},
	)

	// 2. arda.knowledge.search
	if knowledgeTool != nil {
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
				rawArgs, err := json.Marshal(args)
				if err != nil {
					return nil, fmt.Errorf("invalid arguments: %w", err)
				}
				res, err := knowledgeTool.Execute(ctx, scope, rawArgs)
				if err != nil {
					return nil, err
				}
				var parsed any
				if err := json.Unmarshal(res.Data, &parsed); err != nil {
					return string(res.Data), nil
				}
				return parsed, nil
			},
		)
	}

	// 3. arda.crm.exportCustomer (confirm-kind mutation)
	if crmExportTool != nil {
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
				// Confirm kind tools return ErrApprovalRequired to yield a proposal
				return nil, tools.ErrApprovalRequired
			},
		)
	}
}
