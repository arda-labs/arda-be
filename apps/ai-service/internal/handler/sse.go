package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
)

type sseWriter struct {
	writer  *bufio.Writer
	flusher http.Flusher
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
	sse := &sseWriter{writer: bufio.NewWriter(w), flusher: flusher}
	return sse, true
}

func (s *sseWriter) event(payload agentEvent) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(s.writer, "data: %s\n\n", encoded)
	_ = s.writer.Flush()
	s.flusher.Flush()
}

func (s *sseWriter) finalFlush() {
	_ = s.writer.Flush()
	s.flusher.Flush()
}
