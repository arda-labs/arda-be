package catalog

import (
	"context"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// RegisterCRMCatalog registers hand-written CRM SDK methods (arda.crm.*).
// The direct internal read (arda.crm.getCustomer) is generated from
// contracts/ai-internal/crm-v1.json — see RegisterGeneratedCatalog.
//
// arda.crm.exportCustomer stays manual: the CRM service has no export
// backend yet, so the proposal is prepared locally without an HTTP call.
// When the owning team ships /internal/ai/customers/{id}/export with a
// dedicated crm.customer.export permission, move it to the contract and
// delete this entry (see catalog-scale-plan.md WP5/WP6).
func RegisterCRMCatalog(reg *DispatcherRegistry) {
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
 * @requires crm.customer.manage
 * @domain crm
 */`,
			Keywords:            []string{"export", "customer", "csv", "json", "download", "prepare", "xuất dữ liệu", "tải", "file"},
			Kind:                "confirm",
			RequiredPermissions: []string{"crm.customer.manage"},
			Risk:                "medium",
			Timeout:             3 * time.Second,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			customerID, _ := args["customerId"].(string)
			customerID = strings.TrimSpace(customerID)
			if customerID == "" {
				return nil, tools.ErrInvalidArgument
			}
			format, _ := args["format"].(string)
			if strings.TrimSpace(format) == "" {
				format = "csv"
			}
			return map[string]any{
				"status":     "PREPARED",
				"customerId": customerID,
				"format":     format,
				"summary":    "Export for customer " + customerID + " in " + strings.ToUpper(format) + " format is ready for download.",
			}, nil
		},
	)
}
