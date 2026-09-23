package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

// answerFeedbackInput is the POST /api/ai/answers/feedback body.
type answerFeedbackInput struct {
	ThreadID string `json:"thread_id"`
	Helpful  bool   `json:"helpful"`
	Comment  string `json:"comment,omitempty"`
}

// handleAnswerFeedback stores a thumbs up/down on an assistant answer so the
// model can be evaluated and improved later. The rating is attached to the
// conversation (and, when known, the run that produced the answer).
func handleAnswerFeedback(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	feedbackStore, ok := store.(repository.AnswerFeedbackStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.feedback_unavailable")
		return
	}
	var input answerFeedbackInput
	decoder := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_feedback_input")
		return
	}
	threadID := strings.TrimSpace(input.ThreadID)
	if threadID == "" || len(threadID) > 255 {
		problem(w, http.StatusBadRequest, "ai.invalid_feedback_input")
		return
	}
	comment := strings.TrimSpace(input.Comment)
	if len(comment) > 2000 {
		comment = comment[:2000]
	}
	if err := feedbackStore.SaveAnswerFeedback(r.Context(), scope.TenantID, scope.ActorUserID, threadID, input.Helpful, comment); err != nil {
		if errors.Is(err, repository.ErrConversationNotFound) {
			problem(w, http.StatusNotFound, "ai.conversation_not_found")
			return
		}
		problem(w, http.StatusServiceUnavailable, "ai.feedback_unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"result":  map[string]any{"threadId": threadID, "helpful": input.Helpful},
	})
}
