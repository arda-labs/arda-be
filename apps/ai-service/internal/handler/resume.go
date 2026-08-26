package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type executionResolver interface {
	ResolveForExecution(call tools.Call, scope tools.Context) (tools.Tool, tools.Definition, error)
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
	if err := store.Finish(ctx, exec.Run, result.Summary, "SUCCEEDED"); err != nil {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"errors":  []any{},
		"messages": []string{},
		"result": map[string]any{
			"id":      parts[0],
			"status":  "EXECUTED",
			"summary": result.Summary,
			"data":    json.RawMessage(content),
		},
	})
}
