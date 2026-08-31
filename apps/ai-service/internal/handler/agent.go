package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

const modelResultContentLimit = 8 << 10

type definitionSource interface {
	Definitions() []tools.Definition
}

func runAgentStream(
	w http.ResponseWriter,
	r *http.Request,
	store runStore,
	resolver toolResolver,
	scope tools.Context,
	input runInput,
	options RouterOptions,
) {
	ctx := r.Context()
	scopeRun := repository.RunContext{
		TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		ExternalThread: strings.TrimSpace(input.ThreadID), ExternalRun: strings.TrimSpace(input.RunID),
	}
	if err := store.Start(ctx, scopeRun, sanitizeTranscript(latestUserMessage(input.Messages))); err != nil {
		if errors.Is(err, repository.ErrRunAlreadyExists) {
			problem(w, http.StatusConflict, "ai.run_replay")
			return
		}
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}
	sse.event(agentEvent{Type: "RUN_STARTED", ThreadID: input.ThreadID, RunID: input.RunID})

	modelProvider := selectModelProvider(ctx, store, scope, options)
	if modelProvider == nil {
		sse.event(agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID, Error: "ai.model_unavailable"})
		_ = store.Finish(ctx, scopeRun, "Chưa có cấu hình AI model nào được kích hoạt. Vui lòng cấu hình tại trang AI Settings.", "FAILED")
		return
	}

	messages := buildModelMessages(ctx, store, options, scope, scopeRun, latestUserMessage(input.Messages))
	agentStepsLoop(w, r, store, resolver, scope, scopeRun, input, sse, options, modelProvider, messages)
}

// buildIdentityContext renders the minimal actor/tenant/org context injected
// into the model prompt. It deliberately excludes the permission/tool catalog:
// capabilities are discovered through search/execute and authorization is
// enforced at execution time, so dumping them here would just burn tokens and
// go stale.
func buildIdentityContext(scope tools.Context) string {
	var b strings.Builder
	b.WriteString("Current actor:\n")
	if scope.ActorUserID != "" {
		fmt.Fprintf(&b, "- user_id: %s\n", scope.ActorUserID)
	}
	if scope.Username != "" {
		fmt.Fprintf(&b, "- username: %s\n", scope.Username)
	}
	if scope.TenantID != "" {
		fmt.Fprintf(&b, "- tenant_id: %s\n", scope.TenantID)
	}
	if scope.ActiveOrgID != "" {
		fmt.Fprintf(&b, "- org_id: %s\n", scope.ActiveOrgID)
	}
	b.WriteString("\nAuthorization:\n")
	b.WriteString("- Use only capabilities exposed by the tool layer.\n")
	b.WriteString("- Authorization is enforced at execution time.\n")
	return b.String()
}

// selectModelProvider resolves the provider for a run. With persistence, the
// saved tenant configuration is the single source of truth — the env key is
// only a fallback for spike/local mode without a database. Nil means "not
// configured", which surfaces as ai.model_unavailable with guidance.
func selectModelProvider(ctx context.Context, store runStore, scope tools.Context, options RouterOptions) model.Provider {
	settingsStore, ok := store.(repository.TenantSettingsStore)
	if !ok {
		// Spike/local mode without persistence.
		return options.ModelProvider
	}
	tenantSettings, err := settingsStore.GetTenantSettings(ctx, scope.TenantID)
	if err != nil || tenantSettings == nil {
		return nil
	}
	if tenantSettings.BaseURL == "" || tenantSettings.ModelID == "" || !baseURLAllowed(options.ModelBaseURLAllowlist, tenantSettings.BaseURL) {
		return nil
	}
	if options.ModelPool != nil {
		return options.ModelPool.GetClient(scope.TenantID, tenantSettings.BaseURL, tenantSettings.APIKey, tenantSettings.ModelID)
	}
	return model.NewClient(tenantSettings.BaseURL, tenantSettings.APIKey, tenantSettings.ModelID, nil)
}

// agentStepsLoop drives the model↔tool loop shared by fresh runs and resumed
// runs. It owns SSE text framing, step budgeting, tool dispatch, and the
// terminal store.Finish call.
func agentStepsLoop(
	w http.ResponseWriter,
	r *http.Request,
	store runStore,
	resolver toolResolver,
	scope tools.Context,
	scopeRun repository.RunContext,
	input runInput,
	sse *sseWriter,
	options RouterOptions,
	modelProvider model.Provider,
	messages []model.Message,
) {
	ctx := r.Context()
	timer := startAIRunTimer()
	defer timer.observe()
	messageID := "msg-" + input.RunID
	textStarted := false
	startText := func() {
		if !textStarted {
			textStarted = true
			sse.event(agentEvent{Type: "TEXT_MESSAGE_START", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		}
	}
	endText := func() {
		if textStarted {
			sse.event(agentEvent{Type: "TEXT_MESSAGE_END", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		}
	}

	defs := modelToolDefinitions(resolver)
	maxSteps := options.AgentMaxSteps
	if maxSteps <= 0 || maxSteps > 12 {
		maxSteps = 6
	}

	awaitingApproval := false
	for step := 0; step < maxSteps && !awaitingApproval; step++ {
		var turnText strings.Builder
		var turnReasoning strings.Builder
		var collected []model.ToolCall
		finishReason, _, err := modelProvider.StreamChat(ctx, messages, defs, model.StreamCallbacks{
			OnTextDelta: func(delta string) {
				turnText.WriteString(delta)
				startText()
				sse.event(agentEvent{
					Type: "TEXT_MESSAGE_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID,
					MessageID: messageID, Delta: delta,
				})
			},
			OnToolCall: func(call model.ToolCall) {
				collected = append(collected, call)
			},
			OnFinish: func(_ string, usage model.Usage) {
				recordLLMUsage(usage)
			},
			OnReasoningDelta: func(delta string) {
				// Chain-of-thought streams to reasoning-aware clients and is
				// kept on the assistant turn so thinking-mode providers accept
				// the follow-up request within this run. It is never persisted
				// across runs.
				turnReasoning.WriteString(delta)
				sse.event(agentEvent{
					Type: "REASONING_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID,
					MessageID: "rsn-" + input.RunID, Delta: delta,
				})
			},
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Error("LLM model stream failed",
				"err", err,
				"thread_id", input.ThreadID,
				"run_id", input.RunID,
				"tenant_id", scope.TenantID,
				"user_id", scope.ActorUserID,
			)
			endText()
			sse.event(agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID, Error: "ai.model_unavailable"})
			recordRunOutcome("FAILED")
			_ = store.Finish(ctx, scopeRun, fmt.Sprintf("I could not complete that request right now: %v", err), "FAILED")
			return
		}

		if len(collected) == 0 {
			reply := strings.TrimSpace(turnText.String())
			if reply == "" {
				reply = "Tôi chưa có câu trả lời cho yêu cầu này."
				startText()
				sse.event(agentEvent{Type: "TEXT_MESSAGE_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID, Delta: reply})
			}
			endText()
			sse.event(agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID})
			recordRunOutcome("SUCCEEDED")
			_ = store.Finish(ctx, scopeRun, reply, "SUCCEEDED")
			return
		}

		messages = append(messages, model.Message{
			Role:      "assistant",
			Content:   turnText.String(),
			Reasoning: turnReasoning.String(),
			ToolCalls: collected,
		})
		for _, call := range collected {
			if awaitingApproval {
				// A resumed run must pair every emitted tool_call with a tool
				// message or strict providers reject the follow-up request.
				skipped := `{"error":"skipped_pending_approval"}`
				sse.event(agentEvent{
					Type: "TOOL_CALL_RESULT", ThreadID: input.ThreadID, RunID: input.RunID,
					ToolCallID: call.ID, ToolName: call.Name, ToolCallName: call.Name,
					Result: json.RawMessage(skipped), Error: "ai.tool_skipped_pending_approval",
				})
				messages = append(messages, model.Message{Role: "tool", ToolCallID: call.ID, Content: skipped})
				continue
			}
			pending, toolMessage := executeModelToolCall(ctx, r, store, resolver, scope, scopeRun, input, sse, call, options)
			messages = append(messages, model.Message{Role: "tool", ToolCallID: call.ID, Content: toolMessage})
			if pending {
				awaitingApproval = true
			}
		}
		if finishReason == "" && awaitingApproval {
			break
		}
	}

	if awaitingApproval {
		endText()
		sse.event(agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID})
		recordRunOutcome("WAITING_APPROVAL")
		return
	}

	endText()
	reply := "Dừng lại sau số bước xử lý tối đa. Vui lòng thử lại với yêu cầu cụ thể hơn."
	startText()
	sse.event(agentEvent{Type: "TEXT_MESSAGE_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID, Delta: reply})
	sse.event(agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID, Error: "ai.agent_step_limit"})
	recordRunOutcome("FAILED")
	_ = store.Finish(ctx, scopeRun, reply, "FAILED")
}

func buildModelMessages(ctx context.Context, store runStore, options RouterOptions, scope tools.Context, scopeRun repository.RunContext, latestUser string) []model.Message {
	messages := make([]model.Message, 0, 24)
	if prompt := strings.TrimSpace(options.ModelSystemPrompt); prompt != "" {
		messages = append(messages, model.Message{Role: "system", Content: prompt})
	}
	if identity := buildIdentityContext(scope); identity != "" {
		// Minimal identity context: who the actor is and which tenant/org
		// they act in. Deliberately NOT the permission/tool catalog — the
		// model discovers capabilities through search/execute and the
		// runtime enforces authorization at execution time.
		messages = append(messages, model.Message{Role: "system", Content: identity})
	}
	if historyStore, ok := store.(repository.HistoryStore); ok {
		items, err := historyStore.RecentMessages(ctx, scopeRun, 20)
		if err == nil {
			for _, item := range items {
				if item.Content == "" {
					continue
				}
				switch item.Role {
				case "user", "assistant":
					messages = append(messages, model.Message{Role: item.Role, Content: item.Content})
				}
				// Tool history is skipped on replay: HistoryMessage carries no
				// tool_call_id, and providers reject unpaired tool messages.
			}
		}
	}
	messages = append(messages, model.Message{Role: "user", Content: latestUser})
	return messages
}

func modelToolDefinitions(resolver toolResolver) []model.ToolDef {
	source, ok := resolver.(definitionSource)
	if !ok {
		return nil
	}
	definitions := source.Definitions()
	items := make([]model.ToolDef, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, model.ToolDef{
			Name:        definition.Name,
			Description: definition.Description,
			Parameters:  definition.Parameters,
		})
	}
	return items
}

// returns (awaitingApproval, toolFeedbackContent)
func executeModelToolCall(
	ctx context.Context,
	r *http.Request,
	store runStore,
	resolver toolResolver,
	scope tools.Context,
	scopeRun repository.RunContext,
	input runInput,
	sse *sseWriter,
	call model.ToolCall,
	options RouterOptions,
) (bool, string) {
	emit := func(eventType string, payload json.RawMessage, errorCode string) {
		sse.event(agentEvent{
			Type: eventType, ThreadID: input.ThreadID, RunID: input.RunID,
			ToolCallID: call.ID, ToolName: call.Name, ToolCallName: call.Name,
			Result: payload, Error: errorCode,
		})
	}

	selected, definition, err := resolver.Resolve(tools.Call{Name: call.Name, Arguments: json.RawMessage(call.Arguments)}, scope)
	switch {
	case errors.Is(err, tools.ErrUnknownTool):
		emit("TOOL_CALL_RESULT", json.RawMessage(`{"error":"unknown_tool"}`), "ai.tool_not_found")
		return false, `{"error":"unknown_tool"}`
	case errors.Is(err, tools.ErrToolForbidden):
		emit("TOOL_CALL_RESULT", json.RawMessage(`{"error":"forbidden"}`), "ai.tool_forbidden")
		return false, `{"error":"forbidden"}`
	case errors.Is(err, tools.ErrApprovalRequired):
		return createProposalForCall(r, store, scope, scopeRun, input, sse, call, definition, options)
	case err != nil:
		emit("TOOL_CALL_RESULT", json.RawMessage(`{"error":"invalid_arguments"}`), "ai.tool_invalid")
		return false, `{"error":"invalid_arguments"}`
	}

	emit("TOOL_CALL_START", nil, "")
	sse.event(agentEvent{
		Type: "TOOL_CALL_ARGS", ThreadID: input.ThreadID, RunID: input.RunID,
		ToolCallID: call.ID, ToolName: call.Name, ToolCallName: call.Name,
		Delta: call.Arguments,
	})

	toolCtx, cancel := context.WithTimeout(ctx, definition.Timeout)
	defer cancel()
	result, execErr := selected.Execute(toolCtx, scope, json.RawMessage(call.Arguments))

	toolStore, hasToolStore := store.(repository.ToolExecutionStore)
	var executionID string
	if hasToolStore {
		executionID, _ = toolStore.StartTool(ctx, scopeRun, definition.Name, definition.Version, definition.Risk, "allow_model", sanitizeTranscript(call.Arguments))
	}

	content := ""
	errorCode := ""
	if execErr != nil {
		errorCode = toolErrorCode(execErr)
		content = `{"error":"` + errorCode + `"}`
	} else {
		content = boundContent(string(result.Data))
	}
	status := "SUCCEEDED"
	if execErr != nil {
		status = "FAILED"
	}
	if hasToolStore && executionID != "" {
		_ = toolStore.FinishTool(ctx, executionID, status, content, errorCode)
		recordToolOutcome(status, definition.Risk)
	}

	sse.event(agentEvent{
		Type: "TOOL_CALL_END", ThreadID: input.ThreadID, RunID: input.RunID,
		ToolCallID: call.ID, ToolName: call.Name, ToolCallName: call.Name,
		Error: errorCode,
	})
	feedback := content
	if execErr == nil && strings.TrimSpace(result.Summary) != "" {
		feedback = compactToolFeedback(result.Data, result.Summary)
	}
	sse.event(agentEvent{
		Type: "TOOL_CALL_RESULT", ThreadID: input.ThreadID, RunID: input.RunID,
		MessageID: "tool-msg-" + input.RunID, ToolCallID: call.ID,
		Content: feedback, Role: "tool",
	})
	return false, feedback
}

func createProposalForCall(
	r *http.Request,
	store runStore,
	scope tools.Context,
	scopeRun repository.RunContext,
	input runInput,
	sse *sseWriter,
	call model.ToolCall,
	definition tools.Definition,
	options RouterOptions,
) (bool, string) {
	summary := json.RawMessage(`{"denied":"approval_unavailable"}`)
	deniedPayload, _ := json.Marshal(map[string]string{"denied": "approval_unavailable"})

	approvalStore, ok := store.(repository.ApprovalStore)
	if !ok || !options.EnableHITLProposals {
		sse.event(agentEvent{
			Type: "TOOL_CALL_RESULT", ThreadID: input.ThreadID, RunID: input.RunID,
			ToolCallID: call.ID, ToolName: call.Name, ToolCallName: call.Name,
			Result: deniedPayload, Error: "ai.tool_approval_unavailable",
		})
		return false, string(summary)
	}

	key := sha256.Sum256([]byte(strings.Join([]string{scopeRun.ExternalRun, call.Name, call.Arguments}, "|")))
	record, err := approvalStore.CreateApprovalProposal(r.Context(), repository.ApprovalProposal{
		Run:               scopeRun,
		ToolName:          definition.Name,
		ToolVersion:       definition.Version,
		Risk:              definition.Risk,
		ArgumentsRedacted: sanitizeTranscript(call.Arguments),
		SummaryRedacted: mustJSON(map[string]any{
			"action": definition.Name, "arguments": json.RawMessage(sanitizeTranscript(call.Arguments)),
		}),
		ResourceVersion:   "",
		PermissionVersion: strings.TrimSpace(r.Header.Get("X-Auth-Version")),
		ExpiresAt:         time.Now().UTC().Add(15 * time.Minute),
		IdempotencyKey:    hex.EncodeToString(key[:16]),
	})
	if err != nil {
		sse.event(agentEvent{
			Type: "TOOL_CALL_RESULT", ThreadID: input.ThreadID, RunID: input.RunID,
			ToolCallID: call.ID, ToolName: call.Name, ToolCallName: call.Name,
			Result: deniedPayload, Error: "ai.approval_persistence_unavailable",
		})
		return false, string(summary)
	}

	payload := mustJSON(map[string]any{"proposal": map[string]any{
		"id": record.ID, "status": record.Status, "expiresAt": record.ExpiresAt,
	}})
	sse.event(agentEvent{
		Type: "TOOL_CALL_RESULT", ThreadID: input.ThreadID, RunID: input.RunID,
		ToolCallID: call.ID, ToolName: call.Name, ToolCallName: call.Name,
		Result: json.RawMessage(payload),
	})
	feedback := mustJSON(map[string]any{
		"status": "WAITING_APPROVAL", "approvalId": record.ID,
		"note": "Người dùng cần phê duyệt trước khi hành động được thực hiện.",
	})
	return true, feedback
}

func boundContent(value string) string {
	if len(value) > modelResultContentLimit {
		return value[:modelResultContentLimit]
	}
	return value
}

func compactToolFeedback(data json.RawMessage, summary string) string {
	feedback := mustJSON(map[string]any{"summary": summary, "data": json.RawMessage(boundContent(string(data)))})
	return boundContent(feedback)
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
