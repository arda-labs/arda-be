package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/events"
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

const assistantPermission = "ai.assistant.use"
const approvalProposePermission = "ai.approval.propose"
const approvalExecutePermission = "ai.approval.execute"
const knowledgeReadPermission = "ai.knowledge.read"

// ragFeedbacker is the RAG feedback surface used by the handler. Narrow
// interface so the handler never imports the full svcclient package.
type ragFeedbacker interface {
	Feedback(ctx context.Context, md metadata.Context, runID string, helpful bool, comment string) (*svcclient.FeedbackOut, error)
}

type RouterOptions struct {
	EnableHITLProposals bool
	// ModelPool caches tenant model clients and their circuit-breaker state.
	// Tenant model configuration itself lives in ai_tenant_settings (UI).
	ModelPool *model.ClientPool
	// ModelProvider is a development/test fallback used only by stores without
	// TenantSettingsStore persistence. Production leaves it nil.
	ModelProvider     model.Provider
	AgentMaxSteps     int
	ModelSystemPrompt string
	// AgentRunTimeout bounds a whole agent run (model + tools). Zero disables
	// the server-side deadline.
	AgentRunTimeout time.Duration
	// ModelSDKTypes is the generated arda.* TypeScript declaration file
	// injected once into the model context so the model knows the whole SDK
	// surface without re-searching. Empty = not injected (direct-tool mode).
	ModelSDKTypes string
	// ModelSDKTypesProvider renders the model-visible declarations from the
	// current governance state, evaluated per run so a tool disabled at
	// runtime disappears from the model context without a restart (ADR-003).
	// Preferred over ModelSDKTypes when set.
	ModelSDKTypesProvider func() string
	// ToolGovernance evaluates and updates the platform-level enabled/disabled
	// overrides from ai_tool_settings (ADR-003). Nil means no override support
	// (tests, deployments without a database).
	ToolGovernance ToolGovernance
	// ModelBaseURLAllowlist restricts tenant-provided base URLs; empty = disabled.
	ModelBaseURLAllowlist []string
	// ModelGatewayToken is the shared AI Gateway credential (platform secret)
	// applied to tenant model clients and connection tests.
	ModelGatewayToken string
	// ModelSessionSecret derives opaque upstream session identifiers. It must
	// never be sent upstream itself.
	ModelSessionSecret string
	// AllowLocalModelURLs is intended for local development only. Production
	// must keep private and loopback provider addresses blocked to prevent SSRF.
	AllowLocalModelURLs bool
	// RAGClient is the RAG feedback client. Nil means feedback is unavailable.
	// Matches the nil-safe pattern of ModelProvider.
	RAGClient ragFeedbacker
	// RAGService is the in-process knowledge/RAG service for /api/rag/* endpoints.
	RAGService *knowledge.Service
	// CatalogTools is the list of SDK tools surfaced via GET /api/ai/tools.
	CatalogTools []CatalogToolDTO
	// ApprovalResolver resolves confirm-kind tools when executing an approved
	// proposal. Production wires the catalog-backed resolver; when nil the
	// model-tool resolver is used (tests that register confirm tools directly).
	ApprovalResolver executionResolver
	// ProposalTools declares the confirm-kind tools accepted by the
	// FE-initiated proposal endpoint (POST /api/ai/approvals). Built from the
	// registered catalog in main.go so the allowlist cannot drift from it.
	ProposalTools []ProposalToolSpec
	// ReadyCheck lets the process wire database/provider diagnostics into the
	// Kubernetes readiness endpoint without exposing infrastructure details.
	ReadyCheck func(context.Context) error
	// EventPublisher publishes AI lifecycle and audit events (NATS JetStream).
	EventPublisher events.Publisher
}

type CatalogToolDTO struct {
	MethodName          string   `json:"methodName"`
	SDKPath             string   `json:"sdkPath"`
	Domain              string   `json:"domain"`
	Service             string   `json:"service,omitempty"`
	Signature           string   `json:"signature"`
	JSDoc               string   `json:"jsdoc"`
	Keywords            []string `json:"keywords,omitempty"`
	Kind                string   `json:"kind"`
	RequiredPermissions []string `json:"requiredPermissions"`
	Risk                string   `json:"risk"`
	TimeoutMs           int64    `json:"timeoutMs"`
	// Tool governance state (ADR-003). Enabled is the derived effective
	// state; ContractEnabled is the compile-time default from the contract
	// (a hard floor the runtime override can never lift); OverrideEnabled is
	// the runtime override, null when the tool follows the contract.
	Enabled         bool  `json:"enabled"`
	ContractEnabled bool  `json:"contractEnabled"`
	OverrideEnabled *bool `json:"overrideEnabled"`
	// Source identifies the tool origin. "internal" today; "mcp" when the MCP
	// adapter lands (ADR-003 §4).
	Source string `json:"source"`
	// UpdatedBy/UpdatedAt describe the latest runtime override (PATCH
	// responses; empty while the tool follows the contract).
	UpdatedBy string `json:"updatedBy,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ToolGovernance is the handler-facing view of the platform-level tool
// governance (implemented by catalog.Governance). Kept as an interface so the
// handler package does not depend on the catalog package.
type ToolGovernance interface {
	EnsureFresh(ctx context.Context) error
	Snapshot() map[string]bool
	Available() bool
	SetOverride(ctx context.Context, methodName string, enabled bool, actor string) (time.Time, error)
	ClearOverride(ctx context.Context, methodName string, actor string) (time.Time, error)
}

// ProposalToolSpec declares one confirm-kind tool accepted by the FE-initiated
// proposal endpoint. It is built from the registered catalog so the allowlist
// cannot drift from the tool registry.
type ProposalToolSpec struct {
	Name               string
	Version            int
	Risk               string
	RequiredPermission string
}

func findProposalTool(specs []ProposalToolSpec, name string, version int) (ProposalToolSpec, bool) {
	for _, spec := range specs {
		if spec.Name == name && spec.Version == version {
			return spec, true
		}
	}
	return ProposalToolSpec{}, false
}

type runStore interface {
	Start(ctx context.Context, run repository.RunContext, userMessage string) error
	Finish(ctx context.Context, run repository.RunContext, assistantMessage, status string) error
}

type analyticsStore interface {
	GetAnalytics(ctx context.Context, tenantID string) (*repository.AnalyticsSummary, error)
}

type toolResolver interface {
	Resolve(call tools.Call, scope tools.Context) (tools.Tool, tools.Definition, error)
}

type runInput struct {
	// ProtocolVersion is optional for compatibility with AG-UI clients that
	// do not send an Arda extension. When present it is negotiated strictly so
	// a newer client cannot silently interpret an older event contract.
	ProtocolVersion string            `json:"protocolVersion,omitempty"`
	ThreadID        string            `json:"threadId"`
	RunID           string            `json:"runId"`
	Messages        []inputMessage    `json:"messages"`
	State           json.RawMessage   `json:"state"`
	Context         json.RawMessage   `json:"context"`
	Tool            *toolCallInput    `json:"tool,omitempty"`
	Resume          []agUiResumeEntry `json:"resume,omitempty"`
	// ForwardedProps carries per-run client metadata (AG-UI standard field).
	// Arda reads only forwardedProps.ardaContext; everything else is ignored.
	ForwardedProps json.RawMessage `json:"forwardedProps,omitempty"`
}

type inputMessage struct {
	ID      string `json:"id,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type agUiResumeEntry struct {
	InterruptID string          `json:"interruptId"`
	Status      string          `json:"status"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type toolCallInput struct {
	Name      string          `json:"name"`
	Version   int             `json:"version,omitempty"`
	Arguments json.RawMessage `json:"arguments"`
}

type agentEvent struct {
	Type         string          `json:"type"`
	ThreadID     string          `json:"threadId,omitempty"`
	RunID        string          `json:"runId,omitempty"`
	MessageID    string          `json:"messageId,omitempty"`
	ToolCallID   string          `json:"toolCallId,omitempty"`
	ToolName     string          `json:"toolName,omitempty"`
	ToolCallName string          `json:"toolCallName,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	Content      string          `json:"content,omitempty"`
	Role         string          `json:"role,omitempty"`
	Error        string          `json:"error,omitempty"`
}

func NewRouter(stores ...runStore) http.Handler {
	var store runStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return newRouter(store, nil, RouterOptions{})
}

func NewRouterWithDependencies(store runStore, resolver toolResolver) http.Handler {
	return newRouter(store, resolver, RouterOptions{})
}

func NewRouterWithOptions(store runStore, resolver toolResolver, options RouterOptions) http.Handler {
	return newRouter(store, resolver, options)
}

func newRouter(store runStore, resolver toolResolver, options RouterOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", health)
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if options.ReadyCheck != nil {
			if err := options.ReadyCheck(r.Context()); err != nil {
				problem(w, http.StatusServiceUnavailable, "ai.not_ready")
				return
			}
		}
		health(w, r)
	})
	mux.HandleFunc("/api/ai/agent", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, store, resolver, options)
	})
	mux.HandleFunc("/api/ai/feedback", func(w http.ResponseWriter, r *http.Request) {
		createFeedback(w, r, options.RAGClient)
	})
	mux.HandleFunc("/api/ai/approvals", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleListApprovals(w, r, store, options)
			return
		}
		createApproval(w, r, store, options)
	})
	mux.HandleFunc("/api/ai/approvals/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/execution") {
			executeApprovedTool(w, r, store, resolver, options)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/decision") {
			decideApproval(w, r, store, options)
			return
		}
		if r.Method == http.MethodGet {
			handleListApprovals(w, r, store, options)
			return
		}
		problem(w, http.StatusNotFound, "ai.approval_endpoint_not_found")
	})
	mux.HandleFunc("/api/ai/tools", func(w http.ResponseWriter, r *http.Request) {
		handleListTools(w, r, options)
	})
	mux.HandleFunc("/api/ai/tools/", func(w http.ResponseWriter, r *http.Request) {
		handleUpdateTool(w, r, options)
	})
	mux.HandleFunc("/api/ai/analytics/overview", func(w http.ResponseWriter, r *http.Request) {
		handleGetAnalytics(w, r, store, options)
	})
	mux.HandleFunc("/api/ai/settings/profiles", func(w http.ResponseWriter, r *http.Request) {
		handleProfiles(w, r, store, options)
	})
	mux.HandleFunc("/api/ai/settings/profiles/", func(w http.ResponseWriter, r *http.Request) {
		handleProfileByID(w, r, store, options)
	})
	mux.HandleFunc("/api/ai/settings/quotas", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetQuotas(w, r, store)
			return
		}
		handleUpdateQuotas(w, r, store)
	})
	mux.HandleFunc("/api/ai/conversations", func(w http.ResponseWriter, r *http.Request) {
		listConversations(w, r, store, options)
	})
	mux.HandleFunc("/api/ai/conversations/", func(w http.ResponseWriter, r *http.Request) {
		suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/conversations/"), "/")
		if strings.HasSuffix(suffix, "/messages") {
			conversationMessages(w, r, store, options)
			return
		}
		if r.Method == http.MethodDelete {
			deleteConversation(w, r, store, options)
			return
		}
		problem(w, http.StatusNotFound, "ai.conversation_not_found")
	})
	if options.RAGService != nil {
		NewRAGHandler(options.RAGService).RegisterRoutes(mux)
	}
	return mux
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func run(w http.ResponseWriter, r *http.Request, store runStore, resolver toolResolver, options RouterOptions) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	if r.Header.Get("X-Auth-Checked") != "true" {
		problem(w, http.StatusUnauthorized, "ai.auth_required")
		return
	}
	if strings.TrimSpace(r.Header.Get("X-User-Id")) == "" || strings.TrimSpace(r.Header.Get("X-Tenant-Id")) == "" {
		problem(w, http.StatusUnauthorized, "ai.identity_context_required")
		return
	}
	if !hasPermission(r.Header.Get("X-Permissions"), assistantPermission) {
		problem(w, http.StatusForbidden, "ai.assistant_forbidden")
		return
	}

	var input runInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&input); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_run_input")
		return
	}
	if version := strings.TrimSpace(input.ProtocolVersion); version != "" && version != agUIProtocolVersion {
		problem(w, http.StatusBadRequest, "ai.protocol_version_unsupported")
		return
	}
	runInputFlow(w, r, store, resolver, input, options)
}

// runInputFlow executes a fully decoded AG-UI run input.
func runInputFlow(w http.ResponseWriter, r *http.Request, store runStore, resolver toolResolver, input runInput, options RouterOptions) {
	if strings.TrimSpace(input.ThreadID) == "" || strings.TrimSpace(input.RunID) == "" {
		problem(w, http.StatusBadRequest, "ai.run_identifiers_required")
		return
	}
	// AG-UI resume: resume entries from a HITL interrupt are sent in the same
	// /api/ai/agent body. Delegate to the resume handler.
	if len(input.Resume) > 0 {
		runAgentResume(w, r, store, resolver, input, options)
		return
	}
	if !hasUserMessage(input.Messages) {
		problem(w, http.StatusBadRequest, "ai.user_message_required")
		return
	}

	scope := scopeFromRequest(r)
	var selectedTool tools.Tool
	var definition tools.Definition
	if input.Tool != nil {
		if resolver == nil {
			problem(w, http.StatusNotFound, "ai.tool_not_enabled")
			return
		}
		var err error
		selectedTool, definition, err = resolver.Resolve(tools.Call{
			Name: input.Tool.Name, Version: input.Tool.Version, Arguments: input.Tool.Arguments,
		}, scope)
		if err != nil {
			switch {
			case errors.Is(err, tools.ErrToolForbidden):
				problem(w, http.StatusForbidden, "ai.tool_forbidden")
			case errors.Is(err, tools.ErrUnknownTool):
				problem(w, http.StatusNotFound, "ai.tool_not_found")
			default:
				problem(w, http.StatusBadRequest, "ai.tool_invalid")
			}
			return
		}
	} else if store != nil {
		// A persisted run must always enter the model path. Provider selection
		// is handled by runAgentStream (including tenant settings); falling
		// through to a successful protocol placeholder hides configuration
		// failures from both the user and operators.
		runAgentStream(w, r, store, resolver, scope, input, options)
		return
	}

	scopeRun := repository.RunContext{
		TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		ExternalThread: strings.TrimSpace(input.ThreadID), ExternalRun: strings.TrimSpace(input.RunID),
	}
	// Direct tool executions (including the Code Mode `execute` meta-tool) get
	// the run identity on the scope so any HITL proposal they raise resolves
	// the owning ai_runs row.
	scope.ExternalThread = scopeRun.ExternalThread
	scope.ExternalRun = scopeRun.ExternalRun
	if store != nil {
		if err := store.Start(r.Context(), scopeRun, sanitizeTranscript(latestUserMessage(input.Messages))); err != nil {
			if errors.Is(err, repository.ErrRunAlreadyExists) {
				problem(w, http.StatusConflict, "ai.run_replay")
				return
			}
			problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
			return
		}
	}

	if selectedTool != nil {
		var executionID string
		var toolStore repository.ToolExecutionStore
		if store != nil {
			var ok bool
			toolStore, ok = store.(repository.ToolExecutionStore)
			if !ok {
				_ = store.Finish(r.Context(), scopeRun, "AI tool execution is unavailable.", "FAILED")
				problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
				return
			}
			var err error
			executionID, err = toolStore.StartTool(r.Context(), scopeRun, definition.Name, definition.Version, definition.Risk, "allow", redactToolArguments(input.Tool.Arguments))
			if err != nil {
				_ = store.Finish(r.Context(), scopeRun, "AI tool execution is unavailable.", "FAILED")
				problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
				return
			}
		}

		result, toolErr := selectedTool.Execute(r.Context(), scope, input.Tool.Arguments)
		if r.Context().Err() != nil {
			// A direct tool run can outlive the browser request just like the
			// model loop. Preserve the terminal state with a detached timeout;
			// there is no reliable response channel after a disconnect.
			if toolStore != nil {
				persistCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
				_ = toolStore.FinishTool(persistCtx, executionID, "FAILED", `{}`, "ai.run_cancelled")
				cancel()
			}
			persistAgentRunTerminal(r.Context(), store, scopeRun, "Run cancelled by client.", "CANCELLED")
			writeToolStream(w, input, definition, nil, "Run cancelled by client.", "ai.run_cancelled")
			return
		}
		if toolErr != nil {
			if toolStore != nil {
				_ = toolStore.FinishTool(r.Context(), executionID, "FAILED", `{}`, toolErrorCode(toolErr))
			}
			assistantMessage := "I could not complete that read request right now."
			if store != nil {
				if err := store.Finish(r.Context(), scopeRun, assistantMessage, "FAILED"); err != nil {
					problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
					return
				}
			}
			writeToolStream(w, input, definition, nil, assistantMessage, toolErrorCode(toolErr))
			return
		}
		if toolStore != nil {
			if err := toolStore.FinishTool(r.Context(), executionID, "SUCCEEDED", string(result.Data), ""); err != nil {
				_ = store.Finish(r.Context(), scopeRun, "AI tool execution is unavailable.", "FAILED")
				problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
				return
			}
		}
		if store != nil {
			if err := store.Finish(r.Context(), scopeRun, result.Summary, "SUCCEEDED"); err != nil {
				problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
				return
			}
		}
		writeToolStream(w, input, definition, &result, result.Summary, "")
		return
	}

	problem(w, http.StatusServiceUnavailable, "ai.model_unavailable")
}

func writeToolStream(w http.ResponseWriter, input runInput, definition tools.Definition, result *tools.Result, assistantMessage, toolError string) {
	writeStream(w, func(writer *sseWriter) {
		messageID := "msg-" + input.RunID
		toolCallID := "tool-" + input.RunID
		writer.event(agentEvent{Type: "RUN_STARTED", ThreadID: input.ThreadID, RunID: input.RunID})
		writer.event(agentEvent{Type: "TOOL_CALL_START", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, ToolName: definition.Name, ToolCallName: definition.Name})
		writer.event(agentEvent{Type: "TOOL_CALL_ARGS", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, Delta: string(input.Tool.Arguments)})
		toolEvent := agentEvent{Type: "TOOL_CALL_END", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, ToolName: definition.Name, ToolCallName: definition.Name, Error: toolError}
		if result != nil {
			toolEvent.Result = result.Data
		}
		writer.event(toolEvent)
		toolContent := resultContent(result, toolError, assistantMessage)
		writer.event(agentEvent{Type: "TOOL_CALL_RESULT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: "tool-msg-" + input.RunID, ToolCallID: toolCallID, Content: toolContent, Role: "tool"})
		writer.event(agentEvent{Type: "TEXT_MESSAGE_START", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writer.event(agentEvent{Type: "TEXT_MESSAGE_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID, Delta: assistantMessage})
		writer.event(agentEvent{Type: "TEXT_MESSAGE_END", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writer.event(agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID, Error: toolError})
	})
}

func resultContent(result *tools.Result, toolError, assistantMessage string) string {
	if result != nil {
		return string(result.Data)
	}
	if toolError != "" {
		payload, _ := json.Marshal(map[string]string{"error": toolError, "message": assistantMessage})
		return string(payload)
	}
	return "{}"
}

func writeStream(w http.ResponseWriter, emit func(*sseWriter)) {
	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}
	emit(sse)
	sse.finalFlush()
}

type approvalProposalInput struct {
	ThreadID         string            `json:"threadId"`
	RunID            string            `json:"runId"`
	Tool             approvalToolInput `json:"tool"`
	ResourceVersion  string            `json:"resourceVersion,omitempty"`
	IdempotencyKey   string            `json:"idempotencyKey,omitempty"`
	ExpiresInSeconds int               `json:"expiresInSeconds,omitempty"`
}

type approvalToolInput struct {
	Name      string          `json:"name"`
	Version   int             `json:"version,omitempty"`
	Arguments json.RawMessage `json:"arguments"`
}

type approvalDecisionInput struct {
	Decision string `json:"decision"`
}

func createApproval(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if !options.EnableHITLProposals {
		problem(w, http.StatusNotFound, "ai.hitl_not_enabled")
		return
	}
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := approvalScope(w, r, approvalProposePermission)
	if !ok {
		return
	}
	var input approvalProposalInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_approval_input")
		return
	}
	if err := ensureEOF(decoder); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_approval_input")
		return
	}
	if strings.TrimSpace(input.ThreadID) == "" || strings.TrimSpace(input.RunID) == "" {
		problem(w, http.StatusBadRequest, "ai.run_identifiers_required")
		return
	}
	spec, allowlisted := findProposalTool(options.ProposalTools, input.Tool.Name, normalizeVersion(input.Tool.Version))
	if !allowlisted {
		problem(w, http.StatusBadRequest, "ai.proposal_not_allowlisted")
		return
	}
	if !toolGovernanceAllows(r.Context(), options, input.Tool.Name) {
		problem(w, http.StatusForbidden, "ai.tool_disabled")
		return
	}
	if spec.RequiredPermission != "" && !hasPermission(r.Header.Get("X-Permissions"), spec.RequiredPermission) {
		problem(w, http.StatusForbidden, "ai.proposal_forbidden")
		return
	}
	argumentData := input.Tool.Arguments
	if len(argumentData) == 0 {
		argumentData = json.RawMessage(`{}`)
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	if idempotencyKey == "" || len(idempotencyKey) > 255 {
		problem(w, http.StatusBadRequest, "ai.idempotency_key_required")
		return
	}
	expiresIn := input.ExpiresInSeconds
	if expiresIn == 0 {
		expiresIn = 15 * 60
	}
	if expiresIn < 60 || expiresIn > 60*60 {
		problem(w, http.StatusBadRequest, "ai.approval_expiry_invalid")
		return
	}
	resourceVersion := strings.TrimSpace(input.ResourceVersion)
	if len(resourceVersion) > 255 {
		problem(w, http.StatusBadRequest, "ai.resource_version_invalid")
		return
	}
	redactedArguments := redactArgumentsJSON(string(argumentData))
	summaryData, _ := json.Marshal(map[string]any{
		"action":    spec.Name,
		"arguments": json.RawMessage(redactedArguments),
	})
	approvalStore, ok := store.(repository.ApprovalStore)
	if !ok || approvalStore == nil {
		problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		return
	}
	record, err := approvalStore.CreateApprovalProposal(r.Context(), repository.ApprovalProposal{
		Run:               repository.RunContext{TenantID: scope.TenantID, ActorUserID: scope.ActorUserID, ExternalThread: input.ThreadID, ExternalRun: input.RunID},
		ToolName:          spec.Name,
		ToolVersion:       spec.Version,
		Risk:              spec.Risk,
		ArgumentsRedacted: redactedArguments,
		Arguments:         string(argumentData),
		SummaryRedacted:   string(summaryData),
		ResourceVersion:   resourceVersion,
		PermissionVersion: strings.TrimSpace(r.Header.Get("X-Auth-Version")),
		ExpiresAt:         time.Now().UTC().Add(time.Duration(expiresIn) * time.Second),
		IdempotencyKey:    idempotencyKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrApprovalRunNotFound):
			problem(w, http.StatusNotFound, "ai.run_not_found")
		case errors.Is(err, repository.ErrApprovalRunNotAwaiting):
			problem(w, http.StatusConflict, "ai.run_not_awaiting_approval")
		case errors.Is(err, repository.ErrApprovalIdempotencyMatch):
			problem(w, http.StatusConflict, "ai.idempotency_conflict")
		case errors.Is(err, repository.ErrApprovalArgumentsInvalid):
			problem(w, http.StatusBadRequest, "ai.invalid_proposal_arguments")
		default:
			problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		}
		return
	}
	status := http.StatusCreated
	if record.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, record)
}

func decideApproval(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if !options.EnableHITLProposals {
		problem(w, http.StatusNotFound, "ai.hitl_not_enabled")
		return
	}
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := approvalScope(w, r, approvalExecutePermission)
	if !ok {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/approvals/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "decision" || len(parts[0]) > 128 {
		problem(w, http.StatusNotFound, "ai.approval_not_found")
		return
	}
	var input approvalDecisionInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || ensureEOF(decoder) != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_approval_decision")
		return
	}
	if input.Decision != "approve" && input.Decision != "reject" {
		problem(w, http.StatusBadRequest, "ai.invalid_approval_decision")
		return
	}
	approvalStore, ok := store.(repository.ApprovalStore)
	if !ok || approvalStore == nil {
		problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		return
	}
	record, err := approvalStore.DecideApproval(r.Context(), scope.TenantID, parts[0], scope.ActorUserID, input.Decision)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrApprovalNotFound):
			problem(w, http.StatusNotFound, "ai.approval_not_found")
		case errors.Is(err, repository.ErrApprovalSelf):
			problem(w, http.StatusForbidden, "ai.approval_self_forbidden")
		case errors.Is(err, repository.ErrApprovalExpired), errors.Is(err, repository.ErrApprovalState):
			problem(w, http.StatusConflict, "ai.approval_not_pending")
		default:
			problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		}
		return
	}
	if options.EventPublisher != nil {
		_ = options.EventPublisher.Publish(r.Context(), events.SubjectApprovalDecided, events.NewEnvelope(
			events.TypeApprovalDecided,
			scope.TenantID,
			scope.ActorUserID,
			scope.RequestID,
			scope.TraceID,
			"",
			events.ApprovalDecidedData{
				ApprovalID:     parts[0],
				Decision:       input.Decision,
				ApproverUserID: scope.ActorUserID,
				SelfApproval:   false,
			},
		))
	}
	writeJSON(w, http.StatusOK, record)
}

func handleListApprovals(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if !options.EnableHITLProposals {
		problem(w, http.StatusNotFound, "ai.hitl_not_enabled")
		return
	}
	scope, ok := approvalScope(w, r, assistantPermission)
	if !ok {
		return
	}
	approvalStore, ok := store.(repository.ApprovalStore)
	if !ok || approvalStore == nil {
		writeJSON(w, http.StatusOK, []repository.ApprovalDetail{})
		return
	}

	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	list, err := approvalStore.ListApprovals(r.Context(), scope.TenantID, status, limit, offset)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.list_approvals_failed")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// describeTools returns the catalog with governance applied. It copies the
// startup DTOs so RouterOptions stays immutable across requests: contract
// fields come from startup, override state is refreshed per request.
func describeTools(ctx context.Context, options RouterOptions) []CatalogToolDTO {
	out := make([]CatalogToolDTO, len(options.CatalogTools))
	copy(out, options.CatalogTools)
	var overrides map[string]bool
	if options.ToolGovernance != nil {
		_ = options.ToolGovernance.EnsureFresh(ctx)
		overrides = options.ToolGovernance.Snapshot()
	}
	for i := range out {
		override, ok := overrides[out[i].MethodName]
		out[i].Enabled = out[i].ContractEnabled && (!ok || override)
		if ok {
			value := override
			out[i].OverrideEnabled = &value
		} else {
			out[i].OverrideEnabled = nil
		}
	}
	return out
}

func findCatalogTool(toolsList []CatalogToolDTO, methodName string) (CatalogToolDTO, bool) {
	for _, item := range toolsList {
		if item.MethodName == methodName {
			return item, true
		}
	}
	return CatalogToolDTO{}, false
}

func catalogToolMatches(tool CatalogToolDTO, search string) bool {
	haystack := strings.ToLower(tool.MethodName + " " + tool.SDKPath + " " + tool.Domain + " " + tool.JSDoc + " " + strings.Join(tool.Keywords, " "))
	return strings.Contains(haystack, search)
}

// toolGovernanceAllows reports whether the effective state enables a tool.
// Unknown names are allowed here; the caller's allowlist check runs first.
func toolGovernanceAllows(ctx context.Context, options RouterOptions, methodName string) bool {
	if options.ToolGovernance == nil {
		return true
	}
	_ = options.ToolGovernance.EnsureFresh(ctx)
	entry, known := findCatalogTool(options.CatalogTools, methodName)
	if !known {
		return true
	}
	override, hasOverride := options.ToolGovernance.Snapshot()[methodName]
	return entry.ContractEnabled && (!hasOverride || override)
}

func handleListTools(w http.ResponseWriter, r *http.Request, options RouterOptions) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	toolsList := describeTools(r.Context(), options)

	query := r.URL.Query()
	domain := strings.TrimSpace(query.Get("domain"))
	kind := strings.TrimSpace(query.Get("kind"))
	risk := strings.TrimSpace(query.Get("risk"))
	search := strings.ToLower(strings.TrimSpace(query.Get("q")))
	enabledFilter := strings.TrimSpace(query.Get("enabled"))

	filtered := make([]CatalogToolDTO, 0, len(toolsList))
	for _, tool := range toolsList {
		if domain != "" && !strings.EqualFold(tool.Domain, domain) {
			continue
		}
		if kind != "" && !strings.EqualFold(tool.Kind, kind) {
			continue
		}
		if risk != "" && !strings.EqualFold(tool.Risk, risk) {
			continue
		}
		switch enabledFilter {
		case "true":
			if !tool.Enabled {
				continue
			}
		case "false":
			if tool.Enabled {
				continue
			}
		}
		if search != "" && !catalogToolMatches(tool, search) {
			continue
		}
		filtered = append(filtered, tool)
	}

	// Optional limit/cursor paging keeps the endpoint usable as the catalog
	// grows; the response stays a JSON array (backward compatible).
	limit := 0
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			problem(w, http.StatusBadRequest, "ai.invalid_pagination")
			return
		}
		limit = parsed
	}
	if limit > 0 {
		cursor := 0
		if value := strings.TrimSpace(query.Get("cursor")); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				problem(w, http.StatusBadRequest, "ai.invalid_pagination")
				return
			}
			cursor = parsed
		}
		start := min(cursor, len(filtered))
		end := min(start+limit, len(filtered))
		filtered = filtered[start:end]
	}

	writeJSON(w, http.StatusOK, filtered)
}

type updateToolRequest struct {
	Enabled       *bool `json:"enabled"`
	ClearOverride *bool `json:"clearOverride"`
}

// handleUpdateTool serves PATCH /api/ai/tools/{methodName}: set or clear the
// platform-level runtime override (ADR-003). The response is the updated
// catalog entry so the frontend does not need to refetch the list.
func handleUpdateTool(w http.ResponseWriter, r *http.Request, options RouterOptions) {
	if r.Method != http.MethodPatch {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	methodName := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/tools/"), "/")
	if methodName == "" {
		problem(w, http.StatusNotFound, "ai.tool_not_found")
		return
	}
	entry, known := findCatalogTool(options.CatalogTools, methodName)
	if !known {
		problem(w, http.StatusNotFound, "ai.tool_not_found")
		return
	}
	if options.ToolGovernance == nil || !options.ToolGovernance.Available() {
		problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
		return
	}

	var input updateToolRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	if err := ensureEOF(decoder); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	if (input.Enabled == nil) == (input.ClearOverride == nil) {
		problem(w, http.StatusBadRequest, "ai.invalid_tool_update")
		return
	}
	if input.ClearOverride != nil && !*input.ClearOverride {
		problem(w, http.StatusBadRequest, "ai.invalid_tool_update")
		return
	}

	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	_ = options.ToolGovernance.EnsureFresh(r.Context())

	// The contract default is a hard floor: an override can disable, never
	// enable beyond the contract (ADR-003 §2).
	if input.Enabled != nil && *input.Enabled && !entry.ContractEnabled {
		problem(w, http.StatusConflict, "ai.tool_contract_disabled")
		return
	}

	overridesBefore := options.ToolGovernance.Snapshot()
	previousOverride, hadPrevious := overridesBefore[methodName]
	previousEffective := entry.ContractEnabled && (!hadPrevious || previousOverride)

	action := "set"
	var updatedAt time.Time
	var err error
	if input.ClearOverride != nil {
		action = "clear"
		updatedAt, err = options.ToolGovernance.ClearOverride(r.Context(), methodName, scope.ActorUserID)
	} else {
		updatedAt, err = options.ToolGovernance.SetOverride(r.Context(), methodName, *input.Enabled, scope.ActorUserID)
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.tool_update_failed")
		return
	}

	updated, _ := findCatalogTool(describeTools(r.Context(), options), methodName)
	updated.UpdatedBy = scope.ActorUserID
	updated.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)

	if options.EventPublisher != nil {
		_ = options.EventPublisher.Publish(r.Context(), events.SubjectAuditToolGovernanceChanged, events.NewEnvelope(
			events.TypeAuditToolGovernanceChanged,
			scope.TenantID,
			scope.ActorUserID,
			scope.RequestID,
			scope.TraceID,
			"",
			events.AuditToolGovernanceChangedData{
				MethodName:        methodName,
				Action:            action,
				PreviousEffective: previousEffective,
				NewEffective:      updated.Enabled,
				UpdatedBy:         scope.ActorUserID,
			},
		))
	}

	writeJSON(w, http.StatusOK, updated)
}

func handleGetAnalytics(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	tenantID := scope.TenantID
	if as, ok := store.(analyticsStore); ok {
		summary, err := as.GetAnalytics(r.Context(), tenantID)
		if err == nil && summary != nil {
			writeJSON(w, http.StatusOK, summary)
			return
		}
	}
	problem(w, http.StatusServiceUnavailable, "ai.analytics_persistence_unavailable")
}

// identityScope establishes the caller's identity/tenant context without
// re-checking authorization: the auth-gateway policy (policy.yaml) is the
// single source of authorization, and the settings routes there already
// require ai.admin/superadmin/platform.manage.
func identityScope(w http.ResponseWriter, r *http.Request) (tools.Context, bool) {
	if r.Header.Get("X-Auth-Checked") != "true" {
		problem(w, http.StatusUnauthorized, "ai.auth_required")
		return tools.Context{}, false
	}
	if strings.TrimSpace(r.Header.Get("X-User-Id")) == "" || strings.TrimSpace(r.Header.Get("X-Tenant-Id")) == "" {
		problem(w, http.StatusUnauthorized, "ai.identity_context_required")
		return tools.Context{}, false
	}
	return scopeFromRequest(r), true
}

func approvalScope(w http.ResponseWriter, r *http.Request, permission string) (tools.Context, bool) {
	if r.Header.Get("X-Auth-Checked") != "true" {
		problem(w, http.StatusUnauthorized, "ai.auth_required")
		return tools.Context{}, false
	}
	if strings.TrimSpace(r.Header.Get("X-User-Id")) == "" || strings.TrimSpace(r.Header.Get("X-Tenant-Id")) == "" {
		problem(w, http.StatusUnauthorized, "ai.identity_context_required")
		return tools.Context{}, false
	}
	if !hasPermission(r.Header.Get("X-Permissions"), assistantPermission) || !hasPermission(r.Header.Get("X-Permissions"), permission) {
		problem(w, http.StatusForbidden, "ai.approval_forbidden")
		return tools.Context{}, false
	}
	return scopeFromRequest(r), true
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON")
		}
		return err
	}
	return nil
}

func normalizeVersion(version int) int {
	if version == 0 {
		return 1
	}
	return version
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func hasPermission(raw, wanted string) bool {
	for _, value := range strings.Split(raw, ",") {
		if strings.TrimSpace(value) == wanted || strings.TrimSpace(value) == "superadmin" {
			return true
		}
	}
	return false
}

// hasRequestPermission reports whether the gateway-verified request carries a
// permission in either the tenant scope (X-Permissions) or the global scope
// (X-Global-Permissions). Global admins bypass individual permission checks,
// mirroring the auth-gateway policy engine (bff_handler: IsGlobalAdmin skips
// route permissions) while still requiring the X-Auth-Checked identity
// headers. The "superadmin" sentinel is treated as a wildcard by hasPermission.
func hasRequestPermission(r *http.Request, wanted string) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Global-Admin")), "true") {
		return true
	}
	if hasPermission(r.Header.Get("X-Permissions"), wanted) {
		return true
	}
	return hasPermission(r.Header.Get("X-Global-Permissions"), wanted)
}

func permissionSet(raw string) map[string]struct{} {
	permissions := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			permissions[value] = struct{}{}
		}
	}
	return permissions
}

func scopeFromRequest(r *http.Request) tools.Context {
	return tools.Context{
		TenantID:    strings.TrimSpace(r.Header.Get("X-Tenant-Id")),
		ActorUserID: strings.TrimSpace(r.Header.Get("X-User-Id")),
		OrgIDs:      splitHeader(r.Header.Get("X-User-Org-Ids")),
		ActiveOrgID: strings.TrimSpace(r.Header.Get("X-Org-Id")),
		RequestID:   strings.TrimSpace(r.Header.Get("X-Request-Id")),
		TraceID:     strings.TrimSpace(r.Header.Get("X-Trace-Id")),
		Permissions: permissionSet(r.Header.Get("X-Permissions")),
		// Identity context for arda.iam.* — the gateway strips any
		// client-supplied values and re-injects from the trusted session.
		Username:    strings.TrimSpace(r.Header.Get("X-Username")),
		Email:       strings.TrimSpace(r.Header.Get("X-User-Email")),
		Roles:       splitHeader(r.Header.Get("X-Roles")),
		GlobalRoles: splitHeader(r.Header.Get("X-Global-Roles")),
		GlobalAdmin: strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Global-Admin")), "true"),
		AuthVersion: strings.TrimSpace(r.Header.Get("X-Auth-Version")),
	}
}

func splitHeader(raw string) []string {
	var values []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func hasUserMessage(messages []inputMessage) bool {
	for _, message := range messages {
		if message.Role == "user" && strings.TrimSpace(message.Content) != "" {
			return true
		}
	}
	return false
}

func latestUserMessage(messages []inputMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" && strings.TrimSpace(messages[index].Content) != "" {
			return messages[index].Content
		}
	}
	return ""
}

var transcriptSecretPattern = regexp.MustCompile(`(?i)(bearer\s+[^\s,;]+|(?:authorization|arda_sid|arda_did)\s*[:=]\s*(?:bearer\s+)?[^\s,;]+)`)

func sanitizeTranscript(value string) string {
	value = strings.TrimSpace(value)
	value = transcriptSecretPattern.ReplaceAllString(value, "[REDACTED]")
	return truncateRunes(value, 16*1024)
}

// redactArgumentsJSON builds the display/audit copy of tool arguments. It is
// JSON-aware: redaction rewrites string values (never raw text) so the result
// always parses, and an oversized payload degrades to a bounded marker object
// instead of a truncated fragment. The executable payload is stored separately
// (encrypted) — this copy must never be handed back to a tool.
func redactArgumentsJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		// The repository rejects such a proposal before persistence; keep the
		// audit column valid for callers that still want the marker.
		fallback, marshalErr := json.Marshal(map[string]any{
			"redacted":    sanitizeTranscript(raw),
			"invalidJson": true,
		})
		if marshalErr != nil {
			return `{"redacted":true}`
		}
		return string(fallback)
	}
	redacted, err := json.Marshal(redactJSONSecrets(value, 0))
	if err != nil {
		return `{"redacted":true}`
	}
	if len(redacted) <= 16*1024 {
		return string(redacted)
	}
	// 4 KiB of JSON text escapes to at most ~8 KiB, so the marker itself
	// stays under the 16 KiB audit bound.
	bounded, err := json.Marshal(map[string]any{
		"truncated": true,
		"bytes":     len(redacted),
		"preview":   truncateRunes(string(redacted), 4*1024),
	})
	if err != nil {
		return `{"truncated":true}`
	}
	if len(bounded) > 16*1024 {
		return `{"truncated":true}`
	}
	return string(bounded)
}

// redactJSONSecrets walks decoded JSON and redacts secret-looking string
// values in place. Depth is bounded so a hostile payload cannot force deep
// recursion.
func redactJSONSecrets(value any, depth int) any {
	if depth > 32 {
		return "[REDACTED]"
	}
	switch item := value.(type) {
	case string:
		return sanitizeTranscript(item)
	case []any:
		for index := range item {
			item[index] = redactJSONSecrets(item[index], depth+1)
		}
		return item
	case map[string]any:
		for key, entry := range item {
			item[key] = redactJSONSecrets(entry, depth+1)
		}
		return item
	default:
		return value
	}
}

func redactToolArguments(arguments json.RawMessage) string {
	if len(arguments) == 0 {
		return `{}`
	}
	return redactArgumentsJSON(string(arguments))
}

func toolErrorCode(err error) string {
	if errors.Is(err, tools.ErrInvalidArgument) {
		return "ai.tool_invalid_arguments"
	}
	return "ai.tool_execution_failed"
}

func writeEvent(writer *bufio.Writer, event agentEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Fprintf(writer, "data: %s\n\n", payload)
}

func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"type":%q,"title":%q,"status":%d,"code":%q,"message":%q}`,
		ardahttp.ProblemsTypeBaseURL+code, http.StatusText(status), status, code, code)
}
