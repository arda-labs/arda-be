package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"

	"github.com/arda-labs/arda/apps/ai-service/internal/evaluation"
)

// InternalJobsHandler serves the job endpoints invoked by the platform
// scheduler. Job outcomes are always HTTP 200 with a JSON status body; only
// authentication failures use non-2xx codes.
type InternalJobsHandler struct {
	evalSetPath string
	query       evaluation.QueryFunc
}

func NewInternalJobsHandler(evalSetPath string, query evaluation.QueryFunc) *InternalJobsHandler {
	return &InternalJobsHandler{evalSetPath: evalSetPath, query: query}
}

// Register mounts job endpoints. They accept only platform-service, which is
// the scheduler; the global ServiceAuthMiddleware lets that source through
// for /internal/jobs/ only.
func (h *InternalJobsHandler) Register(mux *http.ServeMux, secret string) {
	guard := identity.RequireServiceAuth(secret, "ai-service", identity.AllowedSources("platform-service"))
	mux.Handle("POST /internal/jobs/ai-eval", guard(http.HandlerFunc(h.aiEval)))
}

func writeJob(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (h *InternalJobsHandler) aiEval(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(h.evalSetPath)
	if err != nil {
		writeJob(w, map[string]any{"status": "failed", "error_code": "AI_EVAL_SET_MISSING", "error_message": err.Error()})
		return
	}
	set, err := evaluation.Parse(raw)
	if err != nil {
		writeJob(w, map[string]any{"status": "failed", "error_code": "AI_EVAL_SET_INVALID", "error_message": err.Error()})
		return
	}
	report := evaluation.Run(r.Context(), set, h.query)
	metrics := map[string]any{
		"passed":            report.Passed,
		"failed":            report.Failed,
		"recall_at_k":       report.RecallAtK,
		"citation_coverage": report.CitationCoverage,
		"duration_ms":       report.DurationMs,
	}
	if report.Failed > 0 {
		writeJob(w, map[string]any{
			"status":        "failed",
			"error_code":    "AI_EVAL_FAILED",
			"error_message": fmt.Sprintf("%d/%d case failed", report.Failed, report.Passed+report.Failed),
			"summary":       report,
			"metrics":       metrics,
		})
		return
	}
	writeJob(w, map[string]any{"status": "succeeded", "summary": report, "metrics": metrics})
}
