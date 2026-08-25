package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

const assistantPermission = "ai.assistant.use"
const protocolSpikeMessage = "Arda AI protocol spike is connected. No model or tool was invoked."

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
	Type       string          `json:"type"`
	ThreadID   string          `json:"threadId,omitempty"`
	RunID      string          `json:"runId,omitempty"`
	MessageID  string          `json:"messageId,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Delta      string          `json:"delta,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func NewRouter(stores ...runStore) http.Handler {
	var store runStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return newRouter(store, nil)
}

func NewRouterWithDependencies(store runStore, resolver toolResolver) http.Handler {
	return newRouter(store, resolver)
}

func newRouter(store runStore, resolver toolResolver) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", health)
	mux.HandleFunc("/health/ready", health)
	mux.HandleFunc("/api/ai/agent", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, store, resolver)
	})
	return mux
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func run(w http.ResponseWriter, r *http.Request, store runStore, resolver toolResolver) {
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
	if strings.TrimSpace(input.ThreadID) == "" || strings.TrimSpace(input.RunID) == "" {
		problem(w, http.StatusBadRequest, "ai.run_identifiers_required")
		return
	}
	if !hasUserMessage(input.Messages) {
		problem(w, http.StatusBadRequest, "ai.user_message_required")
		return
	}

	scope := tools.Context{
		TenantID:    strings.TrimSpace(r.Header.Get("X-Tenant-Id")),
		ActorUserID: strings.TrimSpace(r.Header.Get("X-User-Id")),
		OrgIDs:      splitHeader(r.Header.Get("X-User-Org-Ids")),
		ActiveOrgID: strings.TrimSpace(r.Header.Get("X-Org-Id")),
		RequestID:   strings.TrimSpace(r.Header.Get("X-Request-Id")),
		Permissions: permissionSet(r.Header.Get("X-Permissions")),
	}
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
		writeEvent(writer, agentEvent{Type: "TOOL_CALL_START", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, ToolName: definition.Name})
		toolEvent := agentEvent{Type: "TOOL_CALL_END", ThreadID: input.ThreadID, RunID: input.RunID, ToolCallID: toolCallID, ToolName: definition.Name, Error: toolError}
		if result != nil {
			toolEvent.Result = result.Data
		}
		writeEvent(writer, toolEvent)
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_START", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_CONTENT", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID, Delta: assistantMessage})
		writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_END", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
		writeEvent(writer, agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID, Error: toolError})
	})
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
