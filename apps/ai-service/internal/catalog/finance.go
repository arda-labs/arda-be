package catalog

import (
	"context"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// RegisterFinanceCatalog registers finance SDK methods (arda.finance.*) that
// proxy to the finance service internal surface through the typed client.
func RegisterFinanceCatalog(reg *DispatcherRegistry, financeClient *svcclient.FinanceClient) {
	if financeClient == nil || financeClient.BaseURL == "" {
		return
	}

	reg.Register(
		CatalogEntry{
			MethodName: "finance.getAccount",
			SDKPath:    "arda.finance.getAccount",
			Domain:     "finance",
			Signature:  "arda.finance.getAccount(args: { accountId: string }): Promise<Account>;",
			JSDoc: `/**
 * Read a chart-of-accounts entry in the active tenant.
 * @param args.accountId Finance account identifier (max 128 chars)
 * @returns Account { id, code, name, type, normalBalance, currency, isActive }
 * @requires finance.read
 * @domain finance
 */`,
			Keywords:            []string{"finance", "account", "chart", "ledger", "code", "balance", "read", "get"},
			Kind:                "read",
			RequiredPermissions: []string{"finance.read"},
			Risk:                "medium",
			Timeout:             3 * time.Second,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			accountID, _ := args["accountId"].(string)
			return financeClient.GetAccount(ctx, scopeToMetadata(scope), strings.TrimSpace(accountID))
		},
	)
}
