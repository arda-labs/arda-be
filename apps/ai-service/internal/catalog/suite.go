package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/sandbox"
	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// resultPreviewLimit bounds how much raw output is echoed back to the model
// inline; anything larger stays in the sandbox ResultStore and is fetched via
// readResult (Cloudflare code-mode pattern).
const resultPreviewLimit = 1 << 10

type CodeModeSuite struct {
	SearchTool  tools.Tool
	ExecuteTool tools.Tool
	ReadTool    tools.Tool
	Catalog     *Index
	Engine      *sandbox.Engine
	Registry    *DispatcherRegistry
	ResultStore *sandbox.ResultStore
	// TypeDefs is the generated arda.* TypeScript declaration file injected
	// into the model context once per run.
	TypeDefs string
}

// NewCodeModeSuite builds the 3-meta-tool suite (search & execute & readResult)
// backed by the Goja sandbox. Clients carry the service identity and delegate
// subject headers to target services.
func NewCodeModeSuite(
	crmClient *svcclient.CRMClient,
	financeClient *svcclient.FinanceClient,
	hrmClient *svcclient.HRMClient,
	iamClient *svcclient.IAMClient,
	store repository.RunStore,
	enableHITL bool,
	ragClient ragSearcher,
) *CodeModeSuite {
	dispatcherReg := NewDispatcherRegistry()

	RegisterBuiltinCatalog(dispatcherReg, ragClient)
	RegisterGeneratedCatalog(dispatcherReg, ClientSet{
		CRM:     crmClient,
		Finance: financeClient,
		HRM:     hrmClient,
		IAM:     iamClient,
	})
	catalogIndex := NewIndex(dispatcherReg.AllEntries())
	sandboxEngine := sandbox.NewEngine(dispatcherReg)
	resultStore := sandbox.NewResultStore()

	searchTool := tools.NewSearchMetaTool(func(query, domain string, scope tools.Context) (string, int, error) {
		entries := catalogIndex.Search(query, domain, scope, 5)
		return FormatSignatures(entries), len(entries), nil
	})

	executeTool := tools.NewExecuteMetaTool(func(ctx context.Context, scope tools.Context, code string) (map[string]any, error) {
		res, err := sandboxEngine.Execute(ctx, scope, code)
		if err != nil {
			return nil, err
		}

		// Raw output stays in the sandbox store; the model gets a bounded
		// preview plus a resultId to fetch the full data via readResult.
		rawOutput, _ := json.Marshal(res.Output)
		resultID := resultStore.Put(scope.RequestID, rawOutput, res.Logs)

		out := map[string]any{
			"durationMs":    res.DurationMs,
			"methodsCalled": res.MethodsCalled,
			"scriptHash":    res.ScriptHash,
		}
		if resultID != "" {
			out["resultId"] = resultID
		}
		if len(rawOutput) > resultPreviewLimit {
			// Too big to echo inline — tell the model where to read it.
			out["output"] = map[string]any{
				"truncated": true,
				"size":      len(rawOutput),
				"hint":      "call readResult({ resultId }) for the full output",
			}
		} else if len(rawOutput) > 0 {
			out["output"] = json.RawMessage(rawOutput)
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

	readTool := tools.NewReadResultMetaTool(func(ctx context.Context, scope tools.Context, resultID string) (map[string]any, error) {
		data, logs, ok := resultStore.Get(scope.RequestID, resultID)
		if !ok {
			return nil, fmt.Errorf("result %q not found or expired", resultID)
		}
		return map[string]any{
			"output": json.RawMessage(data),
			"logs":   logs,
		}, nil
	})

	return &CodeModeSuite{
		SearchTool:  searchTool,
		ExecuteTool: executeTool,
		ReadTool:    readTool,
		Catalog:     catalogIndex,
		Engine:      sandboxEngine,
		Registry:    dispatcherReg,
		ResultStore: resultStore,
		TypeDefs:    GenerateTypeDefinitions(dispatcherReg.AllEntries()),
	}
}
