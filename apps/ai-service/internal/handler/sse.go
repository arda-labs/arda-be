package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// The SSE dialect is AI SDK UI Message Stream v1
// (https://ai-sdk.dev/docs/ai-sdk-ui/stream-protocol), advertised via the
// x-vercel-ai-ui-message-stream response header. The legacy AG-UI-style
// dialect was removed; backend and frontend must ship together.
type sseWriter struct {
	writer          *bufio.Writer
	flusher         http.Flusher
	started         bool
	reasoning       bool
	reasoningOpenID string
	toolArgs        map[string]*strings.Builder
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("x-vercel-ai-ui-message-stream", "v1")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	return &sseWriter{writer: bufio.NewWriter(w), flusher: flusher}, true
}

func (s *sseWriter) event(payload agentEvent) {
	for _, part := range s.translate(payload) {
		s.writeData(part)
	}
}

func (s *sseWriter) writeData(part uiStreamPart) {
	encoded, err := json.Marshal(part)
	if err != nil {
		return
	}
	fmt.Fprintf(s.writer, "data: %s\n\n", encoded)
	_ = s.writer.Flush()
	s.flusher.Flush()
}

// uiStreamPart is a superset struct for AI SDK UI Message Stream v1 parts;
// omitted fields stay out of the JSON so each part matches the spec shape.
type uiStreamPart struct {
	Type           string          `json:"type"`
	MessageID      string          `json:"messageId,omitempty"`
	ID             string          `json:"id,omitempty"`
	Delta          string          `json:"delta,omitempty"`
	ToolCallID     string          `json:"toolCallId,omitempty"`
	ToolName       string          `json:"toolName,omitempty"`
	InputTextDelta string          `json:"inputTextDelta,omitempty"`
	Input          json.RawMessage `json:"input,omitempty"`
	Output         json.RawMessage `json:"output,omitempty"`
	ErrorText      string          `json:"errorText,omitempty"`
	FinishReason   string          `json:"finishReason,omitempty"`
	ApprovalID     string          `json:"approvalId,omitempty"`
	Reason         string          `json:"reason,omitempty"`
}

// translate maps one legacy agent event to zero or more v2 parts. Field
// shapes follow packages/ai/src/ui-message-stream/ui-message-chunks.ts of
// vercel/ai: tool parts key on toolCallId, text parts on id.
func (s *sseWriter) translate(ev agentEvent) []uiStreamPart {
	switch ev.Type {
	case "RUN_STARTED":
		if s.started {
			return nil
		}
		s.started = true
		return []uiStreamPart{{Type: "start"}, {Type: "start-step"}}
	case "TEXT_MESSAGE_START":
		return []uiStreamPart{{Type: "text-start", ID: ev.MessageID}}
	case "TEXT_MESSAGE_CONTENT":
		return []uiStreamPart{{Type: "text-delta", ID: ev.MessageID, Delta: ev.Delta}}
	case "TEXT_MESSAGE_END":
		return []uiStreamPart{{Type: "text-end", ID: ev.MessageID}}
	case "TOOL_CALL_START":
		if s.toolArgs == nil {
			s.toolArgs = map[string]*strings.Builder{}
		}
		s.toolArgs[ev.ToolCallID] = &strings.Builder{}
		return []uiStreamPart{{Type: "tool-input-start", ToolCallID: ev.ToolCallID, ToolName: ev.ToolName}}
	case "TOOL_CALL_ARGS":
		if builder, ok := s.toolArgs[ev.ToolCallID]; ok {
			builder.WriteString(ev.Delta)
		}
		return []uiStreamPart{{Type: "tool-input-delta", ToolCallID: ev.ToolCallID, InputTextDelta: ev.Delta}}
	case "TOOL_CALL_END":
		input := json.RawMessage(`{}`)
		if builder, ok := s.toolArgs[ev.ToolCallID]; ok {
			if parsed := json.RawMessage(builder.String()); json.Valid(parsed) {
				input = parsed
			}
		}
		return []uiStreamPart{{
			Type: "tool-input-available", ToolCallID: ev.ToolCallID,
			ToolName: ev.ToolName, Input: input,
		}}
	case "TOOL_CALL_RESULT":
		// Approval proposals additionally emit the spec's tool-approval-request
		// part so approval-capable clients can react natively; plain clients
		// keep consuming the proposal payload inside the tool output.
		if proposalID := proposalIDFromResult(ev.Result); proposalID != "" {
			return []uiStreamPart{
				{Type: "tool-approval-request", ToolCallID: ev.ToolCallID, ApprovalID: proposalID, Reason: "human approval required"},
				{Type: "tool-output-available", ToolCallID: ev.ToolCallID, Output: ev.Result},
			}
		}
		output := ev.Result
		if len(output) == 0 {
			if ev.Content == "" {
				output = json.RawMessage(`{}`)
			} else {
				output = json.RawMessage(compactJSONText(ev.Content))
			}
		}
		if ev.Error != "" {
			return []uiStreamPart{{
				Type: "tool-output-error", ToolCallID: ev.ToolCallID, ErrorText: ev.Error,
			}}
		}
		return []uiStreamPart{{
			Type: "tool-output-available", ToolCallID: ev.ToolCallID, Output: output,
		}}
	case "REASONING_CONTENT":
		if ev.Delta == "" {
			return nil
		}
		s.reasoningOpenID = "rsn-" + ev.RunID
		parts := []uiStreamPart{{Type: "reasoning-delta", ID: s.reasoningOpenID, Delta: ev.Delta}}
		if !s.reasoning {
			s.reasoning = true
			return append([]uiStreamPart{{Type: "reasoning-start", ID: s.reasoningOpenID}}, parts...)
		}
		return parts
	case "RUN_FINISHED":
		parts := make([]uiStreamPart, 0, 4)
		if s.reasoning {
			parts = append(parts, uiStreamPart{Type: "reasoning-end", ID: s.reasoningOpenID})
			s.reasoning = false
		}
		parts = append(parts, uiStreamPart{Type: "finish-step"})
		if ev.Error != "" {
			parts = append(parts, uiStreamPart{Type: "error", ErrorText: ev.Error})
		}
		return append(parts, uiStreamPart{Type: "finish", FinishReason: "stop"})
	default:
		return nil
	}
}

func (s *sseWriter) finalFlush() {
	_ = s.writer.Flush()
	s.flusher.Flush()
}

// compactJSONText passes JSON through unchanged and wraps plain strings so
// tool outputs stay object-shaped for the client.
func compactJSONText(value string) json.RawMessage {
	trimmed := strings.TrimSpace(value)
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	encoded, _ := json.Marshal(map[string]string{"text": value})
	return encoded
}

// proposalIDFromResult detects a HITL approval proposal payload
// {"proposal":{"id":...}} and returns its id for tool-approval-request parts.
func proposalIDFromResult(result json.RawMessage) string {
	if len(result) == 0 {
		return ""
	}
	var probe struct {
		Proposal *struct {
			ID string `json:"id"`
		} `json:"proposal"`
	}
	if json.Unmarshal(result, &probe) != nil || probe.Proposal == nil {
		return ""
	}
	return probe.Proposal.ID
}
