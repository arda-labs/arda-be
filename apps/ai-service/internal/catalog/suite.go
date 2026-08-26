package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/sandbox"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type CodeModeSuite struct {
	SearchTool  tools.Tool
	ExecuteTool tools.Tool
	Catalog     *Index
	Engine      *sandbox.Engine
	Registry    *DispatcherRegistry
}

// NewCodeModeSuite builds the complete 2-meta-tool suite (search & execute) backed by the Goja sandbox.
func NewCodeModeSuite(
	crmBaseURL string,
	httpClient *http.Client,
	db *sql.DB,
	store repository.RunStore,
	enableHITL bool,
) *CodeModeSuite {
	dispatcherReg := NewDispatcherRegistry()

	var searcher knowledge.Searcher
	if db != nil {
		searcher = knowledge.NewSQLSearcher(db)
	}

	RegisterBuiltinCatalog(dispatcherReg, crmBaseURL, httpClient, searcher)
	catalogIndex := NewIndex(dispatcherReg.AllEntries())
	sandboxEngine := sandbox.NewEngine(dispatcherReg)

	searchTool := tools.NewSearchMetaTool(func(query, domain string, scope tools.Context) (string, int, error) {
		entries := catalogIndex.Search(query, domain, scope, 5)
		return FormatSignatures(entries), len(entries), nil
	})

	executeTool := tools.NewExecuteMetaTool(func(ctx context.Context, scope tools.Context, code string) (map[string]any, error) {
		res, err := sandboxEngine.Execute(ctx, scope, code)
		if err != nil {
			return nil, err
		}

		out := map[string]any{
			"output":        res.Output,
			"durationMs":    res.DurationMs,
			"methodsCalled": res.MethodsCalled,
			"scriptHash":    res.ScriptHash,
		}
		if len(res.Logs) > 0 {
			out["logs"] = res.Logs
		}

		// If a mutation was called inside the sandbox, persist the ApprovalProposal in DB
		if res.ApprovalNeeded {
			scopeRun := repository.RunContext{
				TenantID:    scope.TenantID,
				ActorUserID: scope.ActorUserID,
			}

			proposalID := "prop-" + res.ScriptHash[:8]
			expiresAt := time.Now().UTC().Add(15 * time.Minute)

			if approvalStore, ok := store.(repository.ApprovalStore); ok && enableHITL {
				rawArgs, _ := json.Marshal(res.ProposalArgs)
				key := sha256.Sum256([]byte(strings.Join([]string{scope.TenantID, res.ProposalTool, string(rawArgs)}, "|")))

				record, createErr := approvalStore.CreateApprovalProposal(ctx, repository.ApprovalProposal{
					Run:               scopeRun,
					ToolName:          res.ProposalTool,
					ToolVersion:       1,
					Risk:              "medium",
					ArgumentsRedacted: string(rawArgs),
					SummaryRedacted:   fmt.Sprintf(`{"action":"%s","arguments":%s}`, res.ProposalTool, string(rawArgs)),
					ExpiresAt:         expiresAt,
					IdempotencyKey:    hex.EncodeToString(key[:16]),
				})
				if createErr == nil {
					proposalID = record.ID
					expiresAt = record.ExpiresAt
				}
			}

			out["approval"] = map[string]any{
				"id":        proposalID,
				"status":    "PENDING",
				"tool":      res.ProposalTool,
				"args":      res.ProposalArgs,
				"expiresAt": expiresAt.Format(time.RFC3339),
			}
			out["status"] = "WAITING_APPROVAL"
		}

		return out, nil
	})

	return &CodeModeSuite{
		SearchTool:  searchTool,
		ExecuteTool: executeTool,
		Catalog:     catalogIndex,
		Engine:      sandboxEngine,
		Registry:    dispatcherReg,
	}
}
