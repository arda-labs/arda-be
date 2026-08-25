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
)

const assistantPermission = "ai.assistant.use"
const protocolSpikeMessage = "Arda AI protocol spike is connected. No model or tool was invoked."

type runStore interface {
	Start(ctx context.Context, run repository.RunContext, userMessage string) error
	Finish(ctx context.Context, run repository.RunContext, assistantMessage, status string) error
}

type runInput struct {
	ThreadID string          `json:"threadId"`
	RunID    string          `json:"runId"`
	Messages []inputMessage  `json:"messages"`
	State    json.RawMessage `json:"state"`
	Context  json.RawMessage `json:"context"`
}

type inputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type agentEvent struct {
	Type      string `json:"type"`
	ThreadID  string `json:"threadId,omitempty"`
	RunID     string `json:"runId,omitempty"`
	MessageID string `json:"messageId,omitempty"`
	Delta     string `json:"delta,omitempty"`
}

func NewRouter(stores ...runStore) http.Handler {
	var store runStore
	if len(stores) > 0 {
		store = stores[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", health)
	mux.HandleFunc("/health/ready", health)
	mux.HandleFunc("/api/ai/agent", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, store)
	})
	return mux
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func run(w http.ResponseWriter, r *http.Request, store runStore) {
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

	userMessage := sanitizeTranscript(latestUserMessage(input.Messages))
	runContext := repository.RunContext{
		TenantID:       strings.TrimSpace(r.Header.Get("X-Tenant-Id")),
		ActorUserID:    strings.TrimSpace(r.Header.Get("X-User-Id")),
		ExternalThread: strings.TrimSpace(input.ThreadID),
		ExternalRun:    strings.TrimSpace(input.RunID),
	}
	if store != nil {
		if err := store.Start(r.Context(), runContext, userMessage); err != nil {
			if errors.Is(err, repository.ErrRunAlreadyExists) {
				problem(w, http.StatusConflict, "ai.run_replay")
				return
			}
			problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
			return
		}
		if err := store.Finish(r.Context(), runContext, protocolSpikeMessage, "SUCCEEDED"); err != nil {
			problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
			return
		}
	}

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
	messageID := "msg-" + input.RunID
	writeEvent(writer, agentEvent{Type: "RUN_STARTED", ThreadID: input.ThreadID, RunID: input.RunID})
	writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_START", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
	writeEvent(writer, agentEvent{
		Type:      "TEXT_MESSAGE_CONTENT",
		ThreadID:  input.ThreadID,
		RunID:     input.RunID,
		MessageID: messageID,
		Delta:     protocolSpikeMessage,
	})
	writeEvent(writer, agentEvent{Type: "TEXT_MESSAGE_END", ThreadID: input.ThreadID, RunID: input.RunID, MessageID: messageID})
	writeEvent(writer, agentEvent{Type: "RUN_FINISHED", ThreadID: input.ThreadID, RunID: input.RunID})
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
