package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// The SSE dialect is AG-UI (agent-gui protocol), the official assistant-ui
// runtime for non-JS backends. Events are JSON objects in `data:` lines,
// separated by \n\n, each carrying a `type` field. @ag-ui/client validates
// and reassembles them (text/tool/reasoning messages, interrupts).
type sseWriter struct {
	writer               *bufio.Writer
	flusher              http.Flusher
	reasoningMessageID   string
	textMessageID        string
	toolArgs             map[string]*strings.Builder
	pendingApprovalID    string
	pendingToolCallID    string
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	return &sseWriter{writer: bufio.NewWriter(w), flusher: flusher}, true
}

func (s *sseWriter) event(payload agentEvent) {
	for _, event := range s.translate(payload) {
		s.writeData(event)
	}
}

func (s *sseWriter) writeData(ev agUiEvent) {
	encoded, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(s.writer, "data: %s\n\n", encoded)
	_ = s.writer.Flush()
	s.flusher.Flush()
}

// agUiEvent is a superset struct for AG-UI protocol events. Zero-valued
// fields are omitted from the JSON so each event matches the spec shape.
type agUiEvent struct {
	Type         string          `json:"type"`
	ThreadID     string          `json:"threadId,omitempty"`
	RunID        string          `json:"runId,omitempty"`
	MessageID    string          `json:"messageId,omitempty"`
	ToolCallID   string          `json:"toolCallId,omitempty"`
	ToolCallName string          `json:"toolCallName,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	Content      string          `json:"content,omitempty"`
	Role         string          `json:"role,omitempty"`
	ErrorText    string          `json:"error,omitempty"`
	Message      string          `json:"message,omitempty"`
	Code         string          `json:"code,omitempty"`
	Outcome      json.RawMessage `json:"outcome,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
}

// translate maps one internal agent event to zero or more AG-UI protocol
// events. The AG-UI client reassembles these into messages (text, tool
// calls, reasoning) and manages message lifecycle.
func (s *sseWriter) translate(ev agentEvent) []agUiEvent {
	switch ev.Type {
	case "RUN_STARTED":
		return []agUiEvent{{
			Type: "RUN_STARTED", ThreadID: ev.ThreadID, RunID: ev.RunID,
		}}
	case "TEXT_MESSAGE_START":
		s.textMessageID = ev.MessageID
		return []agUiEvent{{
			Type: "TEXT_MESSAGE_START", MessageID: ev.MessageID, Role: "assistant",
		}}
	case "TEXT_MESSAGE_CONTENT":
		if s.textMessageID == "" {
			// Agent loop emits content without an explicit start; open the
			// message first so the client has a message to accumulate into.
			s.textMessageID = ev.MessageID
			return []agUiEvent{
				{Type: "TEXT_MESSAGE_START", MessageID: ev.MessageID, Role: "assistant"},
				{Type: "TEXT_MESSAGE_CONTENT", MessageID: ev.MessageID, Delta: ev.Delta},
			}
		}
		return []agUiEvent{{
			Type: "TEXT_MESSAGE_CONTENT", MessageID: ev.MessageID, Delta: ev.Delta,
		}}
	case "TEXT_MESSAGE_END":
		evts := []agUiEvent{{
			Type: "TEXT_MESSAGE_END", MessageID: ev.MessageID,
		}}
		s.textMessageID = ""
		return evts
	case "TOOL_CALL_START":
		if s.toolArgs == nil {
			s.toolArgs = map[string]*strings.Builder{}
		}
		s.toolArgs[ev.ToolCallID] = &strings.Builder{}
		return []agUiEvent{{
			Type: "TOOL_CALL_START", ToolCallID: ev.ToolCallID, ToolCallName: ev.ToolName,
		}}
	case "TOOL_CALL_ARGS":
		if builder, ok := s.toolArgs[ev.ToolCallID]; ok {
			builder.WriteString(ev.Delta)
		}
		return []agUiEvent{{
			Type: "TOOL_CALL_ARGS", ToolCallID: ev.ToolCallID, Delta: ev.Delta,
		}}
	case "TOOL_CALL_END":
		return []agUiEvent{{
			Type: "TOOL_CALL_END", ToolCallID: ev.ToolCallID,
		}}
	case "TOOL_CALL_RESULT":
		content := ev.Content
		if content == "" && len(ev.Result) > 0 {
			content = string(ev.Result)
		}
		if content == "" {
			content = "{}"
		}
		messageID := ev.MessageID
		if messageID == "" {
			messageID = "tool-msg-" + ev.RunID
		}
		// Track HITL proposals so the final RUN_FINISHED can emit an
		// interrupt outcome instead of a plain success.
		if proposalID := proposalIDFromResult(ev.Result); proposalID != "" {
			s.pendingApprovalID = proposalID
			s.pendingToolCallID = ev.ToolCallID
		}
		if ev.Error != "" {
			return []agUiEvent{{
				Type: "TOOL_CALL_RESULT", MessageID: messageID, ToolCallID: ev.ToolCallID,
				Content: content, Role: "tool",
			}}
		}
		return []agUiEvent{{
			Type: "TOOL_CALL_RESULT", MessageID: messageID, ToolCallID: ev.ToolCallID,
			Content: content, Role: "tool",
		}}
	case "REASONING_CONTENT":
		if ev.Delta == "" {
			return nil
		}
		msgID := "rsn-" + ev.RunID
		if s.reasoningMessageID == "" {
			s.reasoningMessageID = msgID
			return []agUiEvent{
				{Type: "REASONING_MESSAGE_START", MessageID: msgID, Role: "assistant"},
				{Type: "REASONING_MESSAGE_CONTENT", MessageID: msgID, Delta: ev.Delta},
			}
		}
		return []agUiEvent{{
			Type: "REASONING_MESSAGE_CONTENT", MessageID: msgID, Delta: ev.Delta,
		}}
	case "RUN_FINISHED":
		evts := make([]agUiEvent, 0, 4)
		if s.reasoningMessageID != "" {
			evts = append(evts, agUiEvent{Type: "REASONING_MESSAGE_END", MessageID: s.reasoningMessageID})
			s.reasoningMessageID = ""
		}
		if s.textMessageID != "" {
			evts = append(evts, agUiEvent{Type: "TEXT_MESSAGE_END", MessageID: s.textMessageID})
			s.textMessageID = ""
		}
		// HITL: a pending approval proposal ends the run as an interrupt so
		// the client can present it and resume via interrupt responses.
		if s.pendingApprovalID != "" {
			interrupts := []map[string]any{{
				"id":         s.pendingApprovalID,
				"reason":     "confirmation",
				"message":    "Tác vụ yêu cầu xác nhận của bạn trước khi thực hiện.",
				"toolCallId": s.pendingToolCallID,
			}}
			outcome, _ := json.Marshal(map[string]any{
				"type":       "interrupt",
				"interrupts": interrupts,
			})
			s.pendingApprovalID = ""
			s.pendingToolCallID = ""
			return append(evts, agUiEvent{
				Type: "RUN_FINISHED", ThreadID: ev.ThreadID, RunID: ev.RunID, Outcome: outcome,
			})
		}
		if ev.Error != "" {
			// RUN_ERROR is terminal for the AG-UI client (it dispatches the
			// run-failed state); no RUN_FINISHED follows.
			return append(evts, agUiEvent{Type: "RUN_ERROR", Message: ev.Error, Code: "run_error"})
		}
		outcome, _ := json.Marshal(map[string]string{"type": "success"})
		return append(evts, agUiEvent{
			Type: "RUN_FINISHED", ThreadID: ev.ThreadID, RunID: ev.RunID, Outcome: outcome,
		})
	default:
		return nil
	}
}

func (s *sseWriter) finalFlush() {
	_ = s.writer.Flush()
	s.flusher.Flush()
}

// proposalIDFromResult detects a HITL approval proposal payload
// {"proposal":{"id":...}} and returns its id.
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