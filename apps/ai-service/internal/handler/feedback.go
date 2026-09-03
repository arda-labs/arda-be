package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// feedbackInput is the POST /api/ai/feedback body. Helpful/comment match the
// rag-service contract field names (snake_case).
type feedbackInput struct {
	RunID   string `json:"run_id"`
	Helpful bool   `json:"helpful"`
	Comment string `json:"comment,omitempty"`
}

// createFeedback relays a run feedback rating to rag-service. The gateway
// policy (ai-feedback) enforces auth/permissions; the X-Auth-* checks below
// are defense-in-depth, mirroring identityScope.
func createFeedback(w http.ResponseWriter, r *http.Request, rag ragFeedbacker) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	if rag == nil {
		problem(w, http.StatusServiceUnavailable, "ai.feedback_unavailable")
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
	var input feedbackInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || ensureEOF(decoder) != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_feedback_input")
		return
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" || len(runID) > 64 {
		problem(w, http.StatusBadRequest, "ai.invalid_feedback_input")
		return
	}
	scope := scopeFromRequest(r)
	// Delegated metadata for rag-service. Only tenant/user identity travels —
	// rag deps.py reads X-Tenant-Id/X-User-Id and scopes feedback by run
	// ownership, not orgs.
	// ponytail: full identity (orgs/roles/perms) propagation when rag-service
	// scopes feedback by org — extend this literal then.
	md := metadata.Context{TenantID: scope.TenantID, UserID: scope.ActorUserID,
		ActorUserID: scope.ActorUserID, RequestID: scope.RequestID, AuthChecked: "true"}
	out, err := rag.Feedback(r.Context(), md, runID, input.Helpful, input.Comment)
	if err != nil {
		var statusErr *svcclient.StatusError
		if errors.As(err, &statusErr) {
			switch statusErr.Status {
			case http.StatusNotFound:
				problem(w, http.StatusNotFound, "ai.feedback_run_not_found")
			case http.StatusUnprocessableEntity:
				problem(w, http.StatusBadRequest, "ai.invalid_feedback_input")
			case http.StatusForbidden:
				problem(w, http.StatusForbidden, "ai.feedback_forbidden")
			default:
				problem(w, http.StatusBadGateway, "ai.feedback_unavailable")
			}
			return
		}
		problem(w, http.StatusBadGateway, "ai.feedback_unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
