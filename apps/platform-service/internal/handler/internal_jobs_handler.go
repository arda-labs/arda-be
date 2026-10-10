package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/service"
)

// EODRunner runs the SYSTEM end-of-day chain. An empty requestedDate means
// the current SYSTEM business date (see EODService.RunSystem).
type EODRunner interface {
	RunSystem(ctx context.Context, requestedDate string) (*service.RunResult, error)
}

// HistoryCleaner prunes scheduled-job run history and the published outbox.
// Declared here so the handler does not import the repository layer; main
// passes the ScheduledJobRepository, which satisfies it.
type HistoryCleaner interface {
	Cleanup(ctx context.Context, now time.Time) (int64, int64, error)
}

// InternalJobsHandler serves the platform-owned job targets that the job
// executor calls with a signed X-Service-Auth header.
type InternalJobsHandler struct {
	eod     EODRunner
	cleaner HistoryCleaner
}

func NewInternalJobsHandler(eod EODRunner, cleaner HistoryCleaner) *InternalJobsHandler {
	return &InternalJobsHandler{eod: eod, cleaner: cleaner}
}

// RegisterInternalJobRoutes mounts the job targets behind wrap (service auth).
func RegisterInternalJobRoutes(mux *http.ServeMux, h *InternalJobsHandler, wrap func(http.Handler) http.Handler) {
	mux.Handle("POST /internal/jobs/eod", wrap(http.HandlerFunc(h.EOD)))
	mux.Handle("POST /internal/jobs/platform-runs-cleanup", wrap(http.HandlerFunc(h.Cleanup)))
}

type jobRequest struct {
	Params map[string]string `json:"params"`
}

// writeJobResult answers with the job response contract. Job outcomes are
// always HTTP 200; failure is carried in "status" so the executor can tell a
// job failure apart from a transport failure.
func writeJobResult(w http.ResponseWriter, status, code, msg string, summary any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "error_code": code, "error_message": msg, "summary": summary})
}

func (h *InternalJobsHandler) EOD(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	// An empty or malformed body means "no params": run for the current date.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req)
	res, err := h.eod.RunSystem(r.Context(), strings.TrimSpace(req.Params["business_date"]))
	if err != nil {
		writeJobResult(w, "failed", "EOD_RUN_FAILED", err.Error(), nil)
		return
	}
	if res == nil {
		writeJobResult(w, "succeeded", "", "", nil)
		return
	}
	var failed []string
	for _, s := range res.Steps {
		if s.Status == "FAILED" {
			failed = append(failed, s.JobCode+": "+s.Error)
		}
	}
	if len(failed) > 0 {
		writeJobResult(w, "failed", "EOD_STEP_FAILED", strings.Join(failed, "; "), res)
		return
	}
	writeJobResult(w, "succeeded", "", "", res)
}

func (h *InternalJobsHandler) Cleanup(w http.ResponseWriter, r *http.Request) {
	runs, outbox, err := h.cleaner.Cleanup(r.Context(), time.Now())
	if err != nil {
		writeJobResult(w, "failed", "CLEANUP_FAILED", err.Error(), nil)
		return
	}
	writeJobResult(w, "succeeded", "", "", map[string]int64{"runs_deleted": runs, "outbox_deleted": outbox})
}
