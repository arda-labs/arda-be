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
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

const assistantPermission = "ai.assistant.use"
const approvalProposePermission = "ai.approval.propose"
const approvalExecutePermission = "ai.approval.execute"
const protocolSpikeMessage = "Arda AI protocol spike is connected. No model or tool was invoked."

type RouterOptions struct {
	EnableHITLProposals bool
	ModelProvider       model.Provider
	ModelPool           *model.ClientPool
	AgentMaxSteps       int
	ModelSystemPrompt   string
}

type runStore interface {
	Start(ctx context.Context, run repository.RunContext, userMessage string) error
	Finish(ctx context.Context, run repository.RunContext, assistantMessage, status string) error
}

type toolResolver interface {
	Resolve(call tools.Call, scope tools.Context) (tools.Tool, tools.Definition, error)
}

type runInput struct {
	ThreadID string          `json:"threadId"`
	RunID    string          `json:"runId"`
	Messages []inputMessage  `json:"messages"`
	State    json.RawMessage `json:"state"`
	Context  json.RawMessage `json:"context"`
	Tool     *toolCallInput  `json:"tool,omitempty"`
}

type inputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	mux.HandleFunc("/health/ready", health)
	mux.HandleFunc("/api/ai/agent", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, store, resolver, options)
	})
	mux.HandleFunc("/api/ai/approvals", func(w http.ResponseWriter, r *http.Request) {
		createApproval(w, r, store, options)
	})
	mux.HandleFunc("/api/ai/approvals/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/execution") {
			executeApprovedTool(w, r, store, resolver, options)
			return
		}
		decideApproval(w, r, store, options)
	})
	mux.HandleFunc("/api/copilotkit", func(w http.ResponseWriter, r *http.Request) {
		copilotkitEndpoint(w, r, store, resolver, options)
	})
	mux.HandleFunc("/api/ai/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetSettings(w, r, store)
			return
		}
		handleUpdateSettings(w, r, store)
	})
	mux.HandleFunc("/api/ai/settings/test", func(w http.ResponseWriter, r *http.Request) {
		handleTestConnection(w, r, store)
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
	runInputFlow(w, r, store, resolver, input, options)
}

// runInputFlow executes a fully decoded AG-UI run input. Shared by the native
// endpoint and the CopilotKit envelope dispatch.
func runInputFlow(w http.ResponseWriter, r *http.Request, store runStore, resolver toolResolver, input runInput, options RouterOptions) {
	if strings.TrimSpace(input.ThreadID) == "" || strings.TrimSpace(input.RunID) == "" {
		problem(w, http.StatusBadRequest, "ai.run_identifiers_required")
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
	} else if store != nil && options.ModelProvider != nil && resolver != nil {
		runAgentStream(w, r, store, resolver, scope, input, options)
		return
	}

	scopeRun := repository.RunContext{
		TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		ExternalThread: strings.TrimSpace(input.ThreadID), ExternalRun: strings.TrimSpace(input.RunID),
	}
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

	if store != nil {
		if err := store.Finish(r.Context(), scopeRun, protocolSpikeMessage, "SUCCEEDED"); err != nil {
			problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
			return
		}
	}
	writeProtocolStream(w, input)
}

func writeProtocolStream(w http.ResponseWriter, input runInput) {
	writeStream(w, func(writer *bufio.Writer) {
		messageID := "msg-" + input.RunID
		writeEvent(writer, agentEvent{Type: "RUN_STARTED", ThreadID: input.ThreadID, RunID: input.RunID})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_START", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID, Delta: protocolSpikeMessage})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_END", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writeEvent(writer, agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID})
	})
}

func writeToolStream(w http.ResponseWriter, input runInput, definition tools.Definition, result *tools.Result, assistantMessage, toolError string) {
	writeStream(w, func(writer *bufio.Writer) {
		messageID := "msg-" + input.RunID
		toolCallID := "tool-" + input.RunID
		writeEvent(writer, agentEvent{Type: "RUN_STARTED", ThreadID: input.ThreadID, RunID: input.RunID})
		writeEvent(writer, agentEvent{Type: "TOOL_CALL_START", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, ToolName: definition.Name, ToolCallName: definition.Name})
		writeEvent(writer, agentEvent{Type: "TOOL_CALL_ARGS", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, Delta: string(input.Tool.Arguments)})
		toolEvent := agentEvent{Type: "TOOL_CALL_END", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, ToolName: definition.Name, ToolCallName: definition.Name, Error: toolError}
		if result != nil {
			toolEvent.Result = result.Data
		}
		writeEvent(writer, toolEvent)
		toolContent := resultContent(result, toolError, assistantMessage)
		writeEvent(writer, agentEvent{Type: "TOOL_CALL_RESULT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: "tool-msg-" + input.RunID, ToolCallID: toolCallID, Content: toolContent, Role: "tool"})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_START", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID, Delta: assistantMessage})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_END", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writeEvent(writer, agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID, Error: toolError})
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

func writeStream(w http.ResponseWriter, emit func(*bufio.Writer)) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	writer := bufio.NewWriter(w)
	emit(writer)
	_ = writer.Flush()
	flusher.Flush()
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

type customerExportProposalArguments struct {
	CustomerID string `json:"customerId"`
	Format     string `json:"format"`
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
	if input.Tool.Name != "crm.customer.export.prepare" || normalizeVersion(input.Tool.Version) != 1 {
		problem(w, http.StatusBadRequest, "ai.proposal_not_allowlisted")
		return
	}
	arguments, err := validateCustomerExportProposal(input.Tool.Arguments)
	if err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_proposal_arguments")
		return
	}
	if !hasPermission(r.Header.Get("X-Permissions"), "crm.customer.read") {
		problem(w, http.StatusForbidden, "ai.proposal_forbidden")
		return
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
	argumentData, _ := json.Marshal(arguments)
	summaryData, _ := json.Marshal(map[string]any{
		"action":     "prepare_customer_export",
		"customerId": arguments.CustomerID,
		"format":     arguments.Format,
	})
	approvalStore, ok := store.(repository.ApprovalStore)
	if !ok || approvalStore == nil {
		problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		return
	}
	record, err := approvalStore.CreateApprovalProposal(r.Context(), repository.ApprovalProposal{
		Run:               repository.RunContext{TenantID: scope.TenantID, ActorUserID: scope.ActorUserID, ExternalThread: input.ThreadID, ExternalRun: input.RunID},
		ToolName:          input.Tool.Name,
		ToolVersion:       1,
		Risk:              "confirm",
		ArgumentsRedacted: string(argumentData),
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
	writeJSON(w, http.StatusOK, record)
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

func validateCustomerExportProposal(raw json.RawMessage) (customerExportProposalArguments, error) {
	var arguments customerExportProposalArguments
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		return customerExportProposalArguments{}, err
	}
	if err := ensureEOF(decoder); err != nil {
		return customerExportProposalArguments{}, err
	}
	arguments.CustomerID = strings.TrimSpace(arguments.CustomerID)
	arguments.Format = strings.ToLower(strings.TrimSpace(arguments.Format))
	if arguments.CustomerID == "" || len(arguments.CustomerID) > 128 || (arguments.Format != "csv" && arguments.Format != "json") {
		return customerExportProposalArguments{}, errors.New("invalid customer export proposal")
	}
	return arguments, nil
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
		Permissions: permissionSet(r.Header.Get("X-Permissions")),
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

var transcriptSecretPattern = regexp.MustCompile(`(?i)(bearer\s+[^\s,;]+|(?:authorization|arda_sid|arda_did)\s*[:=]\s*[^\s,;]+)`)

func sanitizeTranscript(value string) string {
	value = strings.TrimSpace(value)
	value = transcriptSecretPattern.ReplaceAllString(value, "[REDACTED]")
	if len(value) > 16*1024 {
		return value[:16*1024]
	}
	return value
}

func redactToolArguments(arguments json.RawMessage) string {
	if len(arguments) == 0 {
		return `{}`
	}
	return sanitizeTranscript(string(arguments))
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
	_, _ = fmt.Fprintf(w, `{"type":"https://arda.io.vn/problems/%s","status":%d,"code":%q,"message":%q}`, code, status, code, code)
}
