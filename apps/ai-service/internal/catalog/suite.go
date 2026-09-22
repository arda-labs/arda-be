package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/events"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/sandbox"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// resultPreviewLimit bounds the execute() output echoed inline to the model.
// It must be large enough that a typical knowledge-search result (a handful of
// ~1KB chunks) fits without a follow-up readResult round trip — each extra
// round trip burns one agent step against the AI_AGENT_MAX_STEPS budget.
// Genuinely huge outputs still spill into the sandbox ResultStore and are
// fetched via readResult (Cloudflare code-mode pattern).
const resultPreviewLimit = 6 << 10

// previewMaxRows bounds the rows kept in a presentation preview so a large
// report still renders its chart/KPI in the interface; the full data stays in
// the ResultStore and is reachable through readResult.
const previewMaxRows = 50

// presentationPreview keeps a report-presentation payload useful when it is too
// large to echo whole: the chart, KPI cards and first rows survive, and the
// model is told the full output is available via readResult. Without this a big
// report would degrade to only a truncated hint and the chart would disappear.
func presentationPreview(raw []byte) (map[string]any, bool) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	if _, hasChart := obj["chart"]; !hasChart {
		if _, hasKPI := obj["kpis"]; !hasKPI {
			return nil, false
		}
	}
	rows, _ := obj["rows"].([]any)
	kept := rows
	if len(rows) > previewMaxRows {
		kept = rows[:previewMaxRows]
	}
	preview := map[string]any{"truncated": true}
	for _, key := range []string{"render", "report_code", "report_name", "period_code", "org_code", "columns", "kpis", "chart"} {
		if value, ok := obj[key]; ok {
			preview[key] = value
		}
	}
	preview["rows"] = kept
	preview["row_count"] = len(rows)
	if len(rows) > previewMaxRows {
		preview["rows_truncated"] = true
	}
	preview["hint"] = "call readResult({ resultId }) for the full output"
	return preview, true
}

type CodeModeSuite struct {
	SearchTool     tools.Tool
	ExecuteTool    tools.Tool
	ReadTool       tools.Tool
	Catalog        *Index
	Engine         *sandbox.Engine
	Registry       *DispatcherRegistry
	ResultStore    sandbox.ResultStore
	EventPublisher events.Publisher
	// Governance owns the platform-level enabled/disabled overrides
	// (ADR-003). Set via SetGovernance before the suite is served; nil means
	// every entry follows its contract default.
	Governance *Governance
	// UnwiredServices lists contract services referenced by the generated
	// catalog that have no configured base URL in this deployment. A non-empty
	// list is fatal in production (fail closed) and logged elsewhere.
	UnwiredServices []string
}

func (s *CodeModeSuite) SetEventPublisher(p events.Publisher) {
	if s != nil {
		s.EventPublisher = p
	}
}

// SetResultStore swaps the sandbox result store, e.g. for the Redis-backed
// implementation in production so readResult works across replicas and pod
// restarts (ADR-005 §2). Called at startup, before serving.
func (s *CodeModeSuite) SetResultStore(store sandbox.ResultStore) {
	if s != nil && store != nil {
		s.ResultStore = store
	}
}

// resultNamespace scopes stored sandbox results to the conversation rather
// than a single request, so a resultId from an earlier turn still resolves
// (ADR-005 §2). Tenant and actor are part of the key, so guessing another
// user's thread id cannot read their stored output.
func resultNamespace(scope tools.Context) string {
	thread := strings.TrimSpace(scope.ExternalThread)
	if thread == "" {
		thread = strings.TrimSpace(scope.RequestID)
	}
	if thread == "" {
		thread = "anonymous"
	}
	return scope.TenantID + "|" + scope.ActorUserID + "|" + thread
}

// SetGovernance wires tool governance into the registry (execution + model
// surfaces) and the search index. Called once at startup, before serving.
func (s *CodeModeSuite) SetGovernance(gov *Governance) {
	if s == nil {
		return
	}
	s.Governance = gov
	if s.Registry != nil {
		s.Registry.SetEnabledPredicate(gov.IsEnabled)
	}
	if s.Catalog != nil {
		s.Catalog.SetFilter(gov.IsEnabled)
	}
}

// TypeDefinitions renders the model-visible arda.* TypeScript declarations from
// the currently enabled catalog. It is evaluated per run so a tool disabled at
// runtime disappears from the model context without a restart (ADR-003).
func (s *CodeModeSuite) TypeDefinitions() string {
	if s == nil || s.Registry == nil {
		return ""
	}
	return GenerateTypeDefinitions(s.Registry.EnabledEntries())
}

// NewCodeModeSuite builds the 3-meta-tool suite (search & execute & readResult)
// backed by the Goja sandbox. clients carries one signed transport per wired
// service, keyed by the canonical contract service name; the generated catalog
// registers only the entries whose service is present and reports the rest via
// UnwiredServices.
func NewCodeModeSuite(
	clients ClientSet,
	store repository.RunStore,
	enableHITL bool,
	ragClient ragSearcher,
	docsClient docsLookuper,
) *CodeModeSuite {
	dispatcherReg := NewDispatcherRegistry()

	RegisterBuiltinCatalog(dispatcherReg, ragClient)
	RegisterDocsCatalog(dispatcherReg, docsClient)
	unwired := RegisterGeneratedCatalog(dispatcherReg, clients)
	catalogIndex := NewIndex(dispatcherReg.AllEntries())
	sandboxEngine := sandbox.NewEngine(dispatcherReg)
	resultStore := sandbox.NewResultStore()

	suite := &CodeModeSuite{
		Catalog:         catalogIndex,
		Engine:          sandboxEngine,
		Registry:        dispatcherReg,
		ResultStore:     resultStore,
		UnwiredServices: unwired,
	}

	searchTool := tools.NewSearchMetaTool(func(query, domain, detail string, scope tools.Context) (string, int, error) {
		entries := catalogIndex.Search(query, domain, scope, 5)
		if detail == "brief" {
			return FormatSignaturesBrief(entries), len(entries), nil
		}
		return FormatSignatures(entries), len(entries), nil
	})

	executeTool := tools.NewExecuteMetaTool(func(ctx context.Context, scope tools.Context, code string) (map[string]any, error) {
		// Refresh the governance snapshot before building the sandbox SDK
		// surface: a tool disabled at runtime must not be injected into this
		// run (ADR-003). A store error keeps the previous snapshot.
		_ = suite.Governance.EnsureFresh(ctx)
		res, err := sandboxEngine.Execute(ctx, scope, code)
		if err != nil {
			if suite.EventPublisher != nil {
				h := sha256.Sum256([]byte(code))
				_ = suite.EventPublisher.Publish(ctx, events.SubjectAuditSandboxRejected, events.NewEnvelope(
					events.TypeAuditSandboxRejected,
					scope.TenantID,
					scope.ActorUserID,
					scope.RequestID,
					"",
					"",
					events.AuditSandboxRejectedData{
						RejectionReason: err.Error(),
						ScriptHash:      hex.EncodeToString(h[:]),
					},
				))
			}
			return nil, err
		}

		// Raw output stays in the sandbox store; the model gets a bounded
		// preview plus a resultId to fetch the full data via readResult.
		rawOutput, _ := json.Marshal(res.Output)
		resultID := suite.ResultStore.Put(resultNamespace(scope), rawOutput, res.Logs)

		out := map[string]any{
			"durationMs":    res.DurationMs,
			"methodsCalled": res.MethodsCalled,
			"scriptHash":    res.ScriptHash,
		}
		if resultID != "" {
			out["resultId"] = resultID
		}
		if len(rawOutput) > resultPreviewLimit {
			// Too big to echo inline — tell the model where to read it, but keep
			// a chart/KPI-bearing presentation usable in the interface.
			if preview, ok := presentationPreview(rawOutput); ok {
				out["output"] = preview
			} else {
				out["output"] = map[string]any{
					"truncated": true,
					"size":      len(rawOutput),
					"hint":      "call readResult({ resultId }) for the full output",
				}
			}
		} else if len(rawOutput) > 0 {
			out["output"] = json.RawMessage(rawOutput)
		}
		if len(res.Logs) > 0 {
			out["logs"] = res.Logs
		}

		// A confirm-kind SDK call was refused by the sandbox engine and is now
		// converted into a durable approval proposal. Fail closed: without
		// HITL or an approval store there is no proposal, and the model is
		// told the action is unavailable instead of receiving a fake id.
		if res.ApprovalNeeded {
			if !enableHITL {
				return nil, tools.ErrApprovalUnavailable
			}
			approvalStore, ok := store.(repository.ApprovalStore)
			if !ok {
				return nil, tools.ErrApprovalUnavailable
			}

			// The proposal must carry the full run identity: the store
			// resolves ai_runs by (tenant, actor, external_run_id), so a
			// scope without the run id can only fail. Fail loudly and early
			// instead of surfacing ai.run_not_found to the model.
			scopeRun := repository.RunContext{
				TenantID:       scope.TenantID,
				ActorUserID:    scope.ActorUserID,
				ExternalThread: scope.ExternalThread,
				ExternalRun:    scope.ExternalRun,
			}
			if strings.TrimSpace(scopeRun.ExternalRun) == "" {
				return nil, fmt.Errorf("%w: the execute call reached the sandbox without an external run id", repository.ErrApprovalRunContextMissing)
			}
			rawArgs, _ := json.Marshal(res.ProposalArgs)
			key := sha256.Sum256([]byte(strings.Join([]string{scope.TenantID, res.ProposalTool, string(rawArgs)}, "|")))
			risk := res.ProposalRisk
			if strings.TrimSpace(risk) == "" {
				risk = "medium"
			}

			record, createErr := approvalStore.CreateApprovalProposal(ctx, repository.ApprovalProposal{
				Run:               scopeRun,
				ToolName:          res.ProposalTool,
				ToolVersion:       1,
				Risk:              risk,
				ArgumentsRedacted: string(rawArgs),
				Arguments:         string(rawArgs),
				SummaryRedacted:   fmt.Sprintf(`{"action":"%s","arguments":%s}`, res.ProposalTool, string(rawArgs)),
				PermissionVersion: strings.TrimSpace(scope.AuthVersion),
				ExpiresAt:         time.Now().UTC().Add(15 * time.Minute),
				IdempotencyKey:    hex.EncodeToString(key[:16]),
			})
			if createErr != nil {
				return nil, fmt.Errorf("create approval proposal: %w", createErr)
			}

			if suite.EventPublisher != nil {
				_ = suite.EventPublisher.Publish(ctx, events.SubjectApprovalRequested, events.NewEnvelope(
					events.TypeApprovalRequested,
					scope.TenantID,
					scope.ActorUserID,
					scope.RequestID,
					"",
					record.ID,
					events.ApprovalRequestedData{
						ApprovalID:         record.ID,
						ToolName:           res.ProposalTool,
						SummaryRedacted:    fmt.Sprintf(`{"action":"%s"}`, res.ProposalTool),
						RequiredCapability: "ai.approval.execute",
						ExpiresAt:          record.ExpiresAt.Format(time.RFC3339),
					},
				))
			}

			return nil, &tools.ApprovalPendingError{Proposal: tools.ApprovalPending{
				ProposalID: record.ID,
				Tool:       res.ProposalTool,
				Version:    1,
				Risk:       risk,
				Args:       res.ProposalArgs,
				ExpiresAt:  record.ExpiresAt,
			}}
		}

		return out, nil
	})

	readTool := tools.NewReadResultMetaTool(func(ctx context.Context, scope tools.Context, resultID string) (map[string]any, error) {
		data, logs, ok := suite.ResultStore.Get(resultNamespace(scope), resultID)
		if !ok {
			return nil, fmt.Errorf("result %q not found or expired", resultID)
		}
		return map[string]any{
			"output": json.RawMessage(data),
			"logs":   logs,
		}, nil
	})

	suite.SearchTool = searchTool
	suite.ExecuteTool = executeTool
	suite.ReadTool = readTool
	return suite
}
