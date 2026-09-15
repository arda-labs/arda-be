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

type CodeModeSuite struct {
	SearchTool     tools.Tool
	ExecuteTool    tools.Tool
	ReadTool       tools.Tool
	Catalog        *Index
	Engine         *sandbox.Engine
	Registry       *DispatcherRegistry
	ResultStore    *sandbox.ResultStore
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

	searchTool := tools.NewSearchMetaTool(func(query, domain string, scope tools.Context) (string, int, error) {
		entries := catalogIndex.Search(query, domain, scope, 5)
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

			scopeRun := repository.RunContext{
				TenantID:    scope.TenantID,
				ActorUserID: scope.ActorUserID,
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
				SummaryRedacted:   fmt.Sprintf(`{"action":"%s","arguments":%s}`, res.ProposalTool, string(rawArgs)),
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
		data, logs, ok := resultStore.Get(scope.RequestID, resultID)
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
