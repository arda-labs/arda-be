package handler

import (
	"net/http"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// AuditHandler exposes audit query and management endpoints.
type AuditHandler struct {
	svc *service.AuditService
}

// NewAuditHandler creates an audit handler.
func NewAuditHandler(svc *service.AuditService) *AuditHandler {
	return &AuditHandler{svc: svc}
}

// Query returns paginated audit logs.
// GET /api/admin/audit
func (h *AuditHandler) Query(w http.ResponseWriter, r *http.Request) {
	listQuery := ardahttp.ParseListQuery(r.URL.Query())
	page := listQuery.Page
	perPage := listQuery.PerPage
	if perPage > 500 {
		perPage = 500
	}

	eventTypes := r.URL.Query()["event_type"]
	subject := r.URL.Query().Get("subject")
	result := r.URL.Query().Get("result")
	tenantID := r.URL.Query().Get("tenantId")
	sort := firstNonEmpty(listQuery.Sort, r.URL.Query().Get("sort"))

	var from, to time.Time
	if f := r.URL.Query().Get("from"); f != "" {
		from, _ = time.Parse(time.RFC3339, f)
	}
	if t := r.URL.Query().Get("to"); t != "" {
		to, _ = time.Parse(time.RFC3339, t)
	}

	events, total, err := h.svc.Query(r.Context(), repository.QueryParams{
		EventTypes: eventTypes,
		Subject:    subject,
		Result:     result,
		TenantID:   tenantID,
		From:       from,
		To:         to,
		Page:       page,
		Size:       perPage,
		Sort:       sort,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ardahttp.WriteList(w, r, page, perPage, total, events)
}

// Stats returns audit statistics.
// GET /api/admin/audit/stats
func (h *AuditHandler) Stats(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from := now.Add(-24 * time.Hour)
	to := now

	if fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	stats, err := h.svc.Stats(r.Context(), from, to)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, stats)
}

// Verify checks hash chain integrity.
// GET /api/admin/audit/verify
func (h *AuditHandler) Verify(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from := now.Add(-7 * 24 * time.Hour)
	to := now

	if fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	result, err := h.svc.VerifyChain(r.Context(), from, to)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}
