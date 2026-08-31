package catalog

import (
	"context"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// RegisterCRMCatalog registers CRM SDK methods (arda.crm.*) that proxy to the
// CRM service internal surface through the typed client. The dispatchers only
// validate arguments and convert the verified AI scope to delegated subject
// headers; the signed caller identity is added by the transport.
func RegisterCRMCatalog(reg *DispatcherRegistry, crmClient *svcclient.CRMClient) {
	if crmClient == nil || crmClient.BaseURL == "" {
		return
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
			c, err := crmClient.GetCustomer(ctx, scopeToMetadata(scope), strings.TrimSpace(customerID))
			if err != nil {
				return nil, err
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
			customerID, _ := args["customerId"].(string)
			format, _ := args["format"].(string)
			return crmClient.ExportCustomer(ctx, scopeToMetadata(scope), strings.TrimSpace(customerID), format)
		},
	)
}
