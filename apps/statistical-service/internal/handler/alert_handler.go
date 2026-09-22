package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// Proactive reporting surface (step 7): threshold rules over stored indicators
// and the alerts they raise. Rules only compare a computed value to a number —
// they never carry SQL, so a rule can only watch something the indicator engine
// can already produce.

// Rules handles GET (list) and POST (upsert) /api/statistical/indicator-rules.
func (h *IndicatorResultHandler) Rules(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		onlyActive := r.URL.Query().Get("active") == "1"
		rules, err := h.svc.ListRules(r.Context(), tenantID, onlyActive)
		if err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"items": rules, "total": len(rules)})
	case http.MethodPost, http.MethodPut:
		var in repository.IndicatorRule
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "invalid body"))
			return
		}
		in.TenantID = tenantID
		in.CreatedBy = r.Header.Get("X-User-Id")
		saved, err := h.svc.UpsertRule(r.Context(), tenantID, in.CreatedBy, &in)
		if err != nil {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, saved)
	default:
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
	}
}

// Alerts handles GET /api/statistical/indicator-alerts?status=&period_code=.
func (h *IndicatorResultHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	alerts, err := h.svc.ListAlerts(r.Context(), tenantID,
		strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
		strings.TrimSpace(r.URL.Query().Get("period_code")))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"items": alerts, "total": len(alerts)})
}

// AckAlert handles POST /api/statistical/indicator-alerts/{id}/ack.
func (h *IndicatorResultHandler) AckAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	if err := h.svc.AckAlert(r.Context(), tenantID, r.PathValue("id"), r.Header.Get("X-User-Id")); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusNotFound, ardaerrors.New(ardaerrors.CodeNotFound, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]bool{"acknowledged": true})
}

// RunEvaluateRulesJob handles POST /internal/jobs/evaluate-rules (called by the
// platform EOD engine after the indicators are computed). Tenant travels in
// X-Tenant-Id, the period in ?period_code= or the month of ?to_date=.
func (h *IndicatorResultHandler) RunEvaluateRulesJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period_code"))
	if period == "" {
		if toDate := strings.TrimSpace(r.URL.Query().Get("to_date")); len(toDate) >= 7 {
			period = toDate[:7]
		}
	}
	summary, err := h.svc.EvaluateIndicatorRules(r.Context(), tenantID, period)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, summary)
}
