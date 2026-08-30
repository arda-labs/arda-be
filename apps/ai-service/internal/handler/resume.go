package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type executionResolver interface {
	ResolveForExecution(call tools.Call, scope tools.Context) (tools.Tool, tools.Definition, error)
}

// runResumeStore is the capability store needed to continue the agent loop
// after an approved tool executes (LangGraph-style interrupt/resume).
type runResumeStore interface {
	RunMessages(ctx context.Context, run repository.RunContext) ([]repository.HistoryMessage, error)
	ResumeRun(ctx context.Context, run repository.RunContext) error
}

func executeApprovedTool(w http.ResponseWriter, r *http.Request, store runStore, resolver toolResolver, options RouterOptions) {
	if !options.EnableHITLProposals {
		problem(w, http.StatusNotFound, "ai.hitl_not_enabled")
		return
	}
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := approvalScope(w, r, assistantPermission)
	if !ok {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/approvals/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "execution" || len(parts[0]) > 128 {
		problem(w, http.StatusNotFound, "ai.approval_not_found")
		return
	}
	executionStore, hasExecutionStore := store.(repository.ExecutionStore)
	if !hasExecutionStore || resolver == nil {
		problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		return
	}
	resumeResolver, hasResumeResolver := resolver.(executionResolver)
	if !hasResumeResolver {
		problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
		return
	}

	ctx := r.Context()
	exec, err := executionStore.FetchApprovedExecution(ctx, scope.TenantID, parts[0], scope.ActorUserID)
	if err != nil {
		if errors.Is(err, repository.ErrApprovalNotFound) {
			problem(w, http.StatusNotFound, "ai.approval_not_found")
			return
		}
		problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		return
	}

	selected, definition, err := resumeResolver.ResolveForExecution(tools.Call{
		Name: exec.ToolName, Version: exec.ToolVersion, Arguments: json.RawMessage(exec.Arguments),
	}, scope)
	if err != nil || definition.Kind != "confirm" {
		problem(w, http.StatusForbidden, "ai.tool_forbidden")
		return
	}

	toolCtx, cancel := contextWithTimeout(ctx, definition.Timeout)
	defer cancel()
	result, execErr := selected.Execute(toolCtx, scope, json.RawMessage(exec.Arguments))

	toolStore, hasToolStore := store.(repository.ToolExecutionStore)
	if execErr != nil {
		if hasToolStore {
			_ = toolStore.FinishTool(ctx, exec.ExecutionID, "WAITING_APPROVAL", `{}`, toolErrorCode(execErr))
		}
		problem(w, http.StatusBadGateway, "ai.execution_failed")
		return
	}
	content := boundContent(string(result.Data))
	if hasToolStore {
		if err := toolStore.FinishTool(ctx, exec.ExecutionID, "SUCCEEDED", content, ""); err != nil {
			problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
			return
		}
	}

	resumeStore, hasResumeStore := store.(runResumeStore)
	if !hasResumeStore {
		// Minimal persistence (spike fakes): finish the run inline without a
		// resumed agent loop and keep the plain JSON response.
		recordRunOutcome("SUCCEEDED")
		recordToolOutcome("SUCCEEDED", definition.Risk)
		if err := store.Finish(ctx, exec.Run, result.Summary, "SUCCEEDED"); err != nil {
			problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"errors":   []any{},
			"messages": []string{},
			"result": map[string]any{
				"id":      parts[0],
				"status":  "EXECUTED",
				"summary": result.Summary,
				"data":    json.RawMessage(content),
			},
		})
		return
	}

	if err := resumeStore.ResumeRun(ctx, exec.Run); err != nil {
		problem(w, http.StatusConflict, "ai.resume_conflict")
		return
	}

	resumeInput := runInput{
		ThreadID: exec.Run.ExternalThread,
		RunID:    exec.Run.ExternalRun,
	}
	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}
	sse.event(agentEvent{Type: "RUN_STARTED", ThreadID: resumeInput.ThreadID, RunID: resumeInput.RunID})

	modelProvider := selectModelProvider(ctx, store, scope, options)
	if modelProvider == nil {
		sse.event(agentEvent{Type: "RUN_FINISHED", ThreadID: resumeInput.ThreadID, RunID: resumeInput.RunID, Error: "ai.model_unavailable"})
		_ = store.Finish(ctx, exec.Run, "Chưa có cấu hình AI model nào được kích hoạt. Vui lòng cấu hình tại trang AI Settings.", "FAILED")
		return
	}

	messages := buildResumeMessages(ctx, resumeStore, options, exec, content)
	agentStepsLoop(w, r, store, resolver, scope, exec.Run, resumeInput, sse, options, modelProvider, messages)
}

// buildResumeMessages reconstructs the provider conversation for a resumed
// run: system prompt + persisted run messages + the executed tool call and
// its result so the model sees a complete tool_calls/tool pairing.
func buildResumeMessages(
	ctx context.Context,
	store runResumeStore,
	options RouterOptions,
	exec repository.ApprovedExecution,
	toolResultContent string,
) []model.Message {
	messages := make([]model.Message, 0, 32)
	if prompt := strings.TrimSpace(options.ModelSystemPrompt); prompt != "" {
		messages = append(messages, model.Message{Role: "system", Content: prompt})
	}
	if items, err := store.RunMessages(ctx, exec.Run); err == nil {
		for _, item := range items {
			if item.Content == "" {
				continue
			}
			switch item.Role {
			case "user", "assistant":
				messages = append(messages, model.Message{Role: item.Role, Content: item.Content})
			}
		}
	}
	toolCallID := "resume-" + exec.ExecutionID
	messages = append(messages,
		model.Message{
			Role: "assistant",
			ToolCalls: []model.ToolCall{{
				ID: toolCallID, Name: exec.ToolName, Arguments: exec.Arguments,
			}},
		},
		model.Message{Role: "tool", ToolCallID: toolCallID, Content: toolResultContent},
	)
	return messages
}

// runAgentResume continues an interrupted agent loop from AG-UI resume
// entries. The AG-UI client sends interrupt responses ({interruptId, status,
// payload}) in the same /api/ai/agent POST body; each resolved interrupt
// executes the approved tool, then the agent loop resumes streaming AG-UI
// events on the same connection.
func runAgentResume(w http.ResponseWriter, r *http.Request, store runStore, resolver toolResolver, input runInput, options RouterOptions) {
	if !options.EnableHITLProposals {
		problem(w, http.StatusNotFound, "ai.hitl_not_enabled")
		return
	}
	executionStore, hasExecutionStore := store.(repository.ExecutionStore)
	if !hasExecutionStore || resolver == nil {
		problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		return
	}
	resumeResolver, hasResumeResolver := resolver.(executionResolver)
	if !hasResumeResolver {
		problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
		return
	}
	ctx := r.Context()
	scope := scopeFromRequest(r)

	// Execute every resolved interrupt (typically one per run).
	type executedTool struct {
		exec   repository.ApprovedExecution
		content string
	}
	var executed []executedTool
	for _, entry := range input.Resume {
		if entry.Status != "resolved" || entry.InterruptID == "" {
			continue
		}
		exec, err := executionStore.FetchApprovedExecution(ctx, scope.TenantID, entry.InterruptID, scope.ActorUserID)
		if err != nil {
			if errors.Is(err, repository.ErrApprovalNotFound) {
				problem(w, http.StatusNotFound, "ai.approval_not_found")
				return
			}
			problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
			return
		}
		selected, definition, err := resumeResolver.ResolveForExecution(tools.Call{
			Name: exec.ToolName, Version: exec.ToolVersion, Arguments: json.RawMessage(exec.Arguments),
		}, scope)
		if err != nil || definition.Kind != "confirm" {
			problem(w, http.StatusForbidden, "ai.tool_forbidden")
			return
		}
		toolCtx, cancel := contextWithTimeout(ctx, definition.Timeout)
		result, execErr := selected.Execute(toolCtx, scope, json.RawMessage(exec.Arguments))
		cancel()
		if execErr != nil {
			if toolStore, ok := store.(repository.ToolExecutionStore); ok {
				_ = toolStore.FinishTool(ctx, exec.ExecutionID, "WAITING_APPROVAL", `{}`, toolErrorCode(execErr))
			}
			problem(w, http.StatusBadGateway, "ai.execution_failed")
			return
		}
		content := boundContent(string(result.Data))
		if toolStore, ok := store.(repository.ToolExecutionStore); ok {
			if err := toolStore.FinishTool(ctx, exec.ExecutionID, "SUCCEEDED", content, ""); err != nil {
				problem(w, http.StatusServiceUnavailable, "ai.tool_persistence_unavailable")
				return
			}
		}
		executed = append(executed, executedTool{exec: exec, content: content})
	}
	if len(executed) == 0 {
		// All interrupts cancelled — nothing to execute; the run stays ended.
		problem(w, http.StatusBadRequest, "ai.resume_entry_required")
		return
	}

	resumeStore, hasResumeStore := store.(runResumeStore)
	if !hasResumeStore {
		problem(w, http.StatusServiceUnavailable, "ai.approval_persistence_unavailable")
		return
	}
	run := executed[0].exec.Run
	if err := resumeStore.ResumeRun(ctx, run); err != nil {
		problem(w, http.StatusConflict, "ai.resume_conflict")
		return
	}

	resumeInput := runInput{
		ThreadID: run.ExternalThread,
		RunID:    run.ExternalRun,
	}
	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}
	sse.event(agentEvent{Type: "RUN_STARTED", ThreadID: resumeInput.ThreadID, RunID: resumeInput.RunID})

	modelProvider := selectModelProvider(ctx, store, scope, options)
	if modelProvider == nil {
		sse.event(agentEvent{Type: "RUN_FINISHED", ThreadID: resumeInput.ThreadID, RunID: resumeInput.RunID, Error: "ai.model_unavailable"})
		_ = store.Finish(ctx, run, "Chưa có cấu hình AI model nào được kích hoạt. Vui lòng cấu hình tại trang AI Settings.", "FAILED")
		return
	}

	messages := buildResumeMessages(ctx, resumeStore, options, executed[0].exec, executed[0].content)
	agentStepsLoop(w, r, store, resolver, scope, run, resumeInput, sse, options, modelProvider, messages)
}
