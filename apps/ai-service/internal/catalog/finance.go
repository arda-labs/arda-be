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

// RegisterFinanceCatalog registers finance SDK methods (arda.finance.*) that
// proxy to the finance service with delegated identity headers.
func RegisterFinanceCatalog(reg *DispatcherRegistry, financeBaseURL string, httpClient *http.Client) {
	if strings.TrimSpace(financeBaseURL) == "" {
		return
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	cleanURL := strings.TrimRight(strings.TrimSpace(financeBaseURL), "/")

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
			accountID = strings.TrimSpace(accountID)
			if accountID == "" || len(accountID) > 128 {
				return nil, fmt.Errorf("accountId is required (max 128 characters)")
			}
			target := cleanURL + "/api/finance/accounts/" + url.PathEscape(accountID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return nil, fmt.Errorf("create finance request: %w", err)
			}
			setDelegatedHeaders(req, scope)
			resp, err := httpClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("finance request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
				return nil, fmt.Errorf("finance returned status %d", resp.StatusCode)
			}
			var account map[string]any
			if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&account); err != nil {
				return nil, fmt.Errorf("decode finance response: %w", err)
			}
			return account, nil
		},
	)
}

// setDelegatedHeaders forwards the gateway-injected identity context to the
// target service. The target treats these headers as already authorized — the
// gateway authenticated the request and the AI service re-checks permissions
// before dispatching.
func setDelegatedHeaders(req *http.Request, scope tools.Context) {
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
}
