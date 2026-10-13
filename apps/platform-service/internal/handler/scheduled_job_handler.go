package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
)

// ScheduledJobAPI is the subset of service.ScheduledJobService the HTTP
// surface needs; tests substitute a fake.
type ScheduledJobAPI interface {
	List(ctx context.Context) ([]domain.ScheduledJob, error)
	Get(ctx context.Context, code string) (*domain.ScheduledJob, error)
	Update(ctx context.Context, code string, in domain.JobConfigUpdate, tenantID, actor string) (*domain.ScheduledJob, error)
	RunNow(ctx context.Context, code, actor string, params map[string]string) (*domain.ScheduledJobRun, error)
	Preview(expr, tz string) ([]time.Time, error)
	ListRuns(ctx context.Context, code, status string, limit, offset int) ([]domain.ScheduledJobRun, int, error)
	GetRun(ctx context.Context, id string) (*domain.ScheduledJobRun, error)
}

type ScheduledJobHandler struct{ svc ScheduledJobAPI }

func NewScheduledJobHandler(svc ScheduledJobAPI) *ScheduledJobHandler {
	return &ScheduledJobHandler{svc: svc}
}

// RegisterScheduledJobRoutes mounts the admin scheduled-jobs API. Gateway
// policy (platform-jobs-read / platform-jobs-manage) gates the routes; writes
// additionally require the global-admin capability here.
func RegisterScheduledJobRoutes(mux *http.ServeMux, h *ScheduledJobHandler) {
	mux.HandleFunc("GET /api/platform/scheduled-jobs", h.List)
	mux.HandleFunc("POST /api/platform/scheduled-jobs/cron-preview", h.Preview)
	mux.HandleFunc("GET /api/platform/scheduled-jobs/{code}", h.Get)
	mux.HandleFunc("PATCH /api/platform/scheduled-jobs/{code}", h.Update)
	mux.HandleFunc("POST /api/platform/scheduled-jobs/{code}/run", h.Run)
	// GET runs/{id} and GET {code}/runs overlap on "/runs/runs" and neither is
	// more specific, so net/http.ServeMux panics if both are registered. One
	// two-segment pattern dispatches them instead ("runs" is never a job code).
	mux.HandleFunc("GET /api/platform/scheduled-jobs/{first}/{second}", h.getTwoSegment)
}

func (h *ScheduledJobHandler) getTwoSegment(w http.ResponseWriter, r *http.Request) {
	first, second := r.PathValue("first"), r.PathValue("second")
	switch {
	case first == "runs":
		r.SetPathValue("id", second)
		h.GetRun(w, r)
	case second == "runs":
		r.SetPathValue("code", first)
		h.ListRuns(w, r)
	default:
		ardahttp.WriteErrorCode(w, r, http.StatusNotFound, ardaerrors.CodeNotFound, "not found")
	}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func writeJobError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrJobNotFound):
		ardahttp.WriteErrorCode(w, r, http.StatusNotFound, "platform.jobs.not_found", err.Error())
	case errors.Is(err, service.ErrJobVersionConflict):
		ardahttp.WriteErrorCode(w, r, http.StatusConflict, "platform.jobs.version_conflict", err.Error())
	case errors.Is(err, service.ErrJobAlreadyRunning):
		ardahttp.WriteErrorCode(w, r, http.StatusConflict, "platform.jobs.already_running", err.Error())
	case errors.Is(err, service.ErrJobRetired):
		ardahttp.WriteErrorCode(w, r, http.StatusConflict, "platform.jobs.retired", err.Error())
	case errors.Is(err, service.ErrSchedulerBusy):
		ardahttp.WriteErrorCode(w, r, http.StatusConflict, "platform.jobs.busy", err.Error())
	case errors.Is(err, service.ErrJobTargetNotConfigured):
		ardahttp.WriteErrorCode(w, r, http.StatusUnprocessableEntity, "platform.jobs.target_not_configured", err.Error())
	case errors.Is(err, service.ErrJobParamNotAllowed):
		ardahttp.WriteErrorCode(w, r, http.StatusBadRequest, "platform.jobs.param_not_allowed", err.Error())
	case errors.Is(err, domain.ErrInvalidCron), errors.Is(err, domain.ErrInvalidTimezone), errors.Is(err, domain.ErrFieldOutOfRange):
		ardahttp.WriteErrorCode(w, r, http.StatusBadRequest, "platform.jobs.invalid_schedule", err.Error())
	default:
		slog.ErrorContext(r.Context(), "scheduled job request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, "internal error"))
	}
}

func writeInvalidJSON(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
}

func (h *ScheduledJobHandler) List(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.svc.List(r.Context())
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, jobs)
}

func (h *ScheduledJobHandler) Get(w http.ResponseWriter, r *http.Request) {
	job, err := h.svc.Get(r.Context(), r.PathValue("code"))
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, job)
}

func (h *ScheduledJobHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !requireGlobalAdmin(w, r) {
		return
	}
	var in domain.JobConfigUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	job, err := h.svc.Update(r.Context(), r.PathValue("code"), in,
		strings.TrimSpace(r.Header.Get("X-Tenant-Id")), strings.TrimSpace(r.Header.Get("X-User-Id")))
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, job)
}

func (h *ScheduledJobHandler) Run(w http.ResponseWriter, r *http.Request) {
	if !requireGlobalAdmin(w, r) {
		return
	}
	var in struct {
		Params map[string]string `json:"params"`
	}
	// An empty body means "run with no params"; anything else must be valid JSON.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeInvalidJSON(w, r)
		return
	}
	run, err := h.svc.RunNow(r.Context(), r.PathValue("code"), strings.TrimSpace(r.Header.Get("X-User-Id")), in.Params)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusAccepted, run)
}

func (h *ScheduledJobHandler) Preview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CronExpr string `json:"cron_expr"`
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	runs, err := h.svc.Preview(in.CronExpr, in.Timezone)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	if runs == nil {
		runs = []time.Time{}
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"next_runs": runs})
}

func (h *ScheduledJobHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lq := ardahttp.ParseListQuery(q)
	// all=1 / view=tree lift the cap in ParseListQuery; run history is always paged.
	if lq.PerPage > ardahttp.MaxPerPage {
		lq.PerPage = ardahttp.MaxPerPage
	}
	runs, total, err := h.svc.ListRuns(r.Context(), r.PathValue("code"), strings.TrimSpace(q.Get("status")), lq.PerPage, lq.Offset())
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeList(w, r, http.StatusOK, lq.Page, lq.PerPage, total, runs)
}

func (h *ScheduledJobHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		ardahttp.WriteErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "run id must be a UUID")
		return
	}
	run, err := h.svc.GetRun(r.Context(), id)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, run)
}
