package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type CalendarHandler struct {
	service *service.CalendarService
	eod     *service.EODService
}

func NewCalendarHandler(svc *service.CalendarService, eod ...*service.EODService) *CalendarHandler {
	h := &CalendarHandler{service: svc}
	if len(eod) > 0 {
		h.eod = eod[0]
	}
	return h
}

func (h *CalendarHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	branchCode := r.URL.Query().Get("branchCode")
	if branchCode == "" {
		branchCode = "HEAD_OFFICE"
	}

	sd, err := h.service.GetSystemDate(r.Context(), branchCode)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, "common.error.internal", err.Error())
		return
	}

	if sd == nil {
		writeErrorCode(w, http.StatusNotFound, "calendar.error.not_found", "system date config not found")
		return
	}

	writeResultWithRequest(w, r, sd, nil)
}

func (h *CalendarHandler) TriggerEOD(w http.ResponseWriter, r *http.Request) {
	// EOD shifts the business date in plt_system_dates, which is global (one
	// row per branch code, default HEAD_OFFICE) and therefore affects every
	// tenant. The gateway only grants platform.manage per route, so the
	// in-service global-admin check is the tenant/global boundary here.
	if !requireGlobalAdmin(w, r) {
		return
	}
	if h.eod == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "calendar.error.eod_unavailable", "EOD orchestrator is unavailable")
		return
	}
	result, err := h.eod.RunSystem(r.Context(), r.URL.Query().Get("business_date"))
	if err != nil {
		if errors.Is(err, domain.ErrEODInProgress) {
			ardahttp.WriteProblem(w, r, http.StatusConflict, ardaerrors.New(ardaerrors.CodeConflict, err.Error()))
			return
		}
		if errors.Is(err, service.ErrEODBusinessDateUnavailable) {
			ardahttp.WriteProblem(w, r, http.StatusServiceUnavailable, ardaerrors.New(ardaerrors.CodeBadGateway, err.Error()))
			return
		}
		if errors.Is(err, service.ErrEODInvalidBusinessDate) {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
			return
		}
		if errors.Is(err, service.ErrEODRunInProgress) {
			ardahttp.WriteProblem(w, r, http.StatusConflict, ardaerrors.New(ardaerrors.CodeConflict, err.Error()))
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "calendar.error.eod_failed", err.Error())
		return
	}

	writeResultWithRequest(w, r, map[string]any{
		"message": "EOD completed successfully",
		"data":    result.BusinessDateState,
	}, nil)
}

func (h *CalendarHandler) EvaluateDate(w http.ResponseWriter, r *http.Request) {
	channelCode := r.URL.Query().Get("channel")
	txnType := r.URL.Query().Get("type")
	execTimeStr := r.URL.Query().Get("time")

	if channelCode == "" || txnType == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "channel and type parameters are required")
		return
	}

	execTime := time.Now()
	if execTimeStr != "" {
		var err error
		execTime, err = time.Parse(time.RFC3339, execTimeStr)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, "validation.invalid_time", "invalid time format, use RFC3339")
			return
		}
	}

	accountingDate, err := h.service.EvaluateAccountingDate(r.Context(), "HEAD_OFFICE", channelCode, txnType, execTime)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, "common.error.internal", err.Error())
		return
	}

	writeResultWithRequest(w, r, map[string]any{
		"channel":        channelCode,
		"type":           txnType,
		"executionTime":  execTime,
		"accountingDate": accountingDate.Format("2006-01-02"),
	}, nil)
}

func (h *CalendarHandler) ListHolidays(w http.ResponseWriter, r *http.Request) {
	holidays, err := h.service.ListHolidays(r.Context())
	writeResultWithRequest(w, r, holidays, err)
}

func (h *CalendarHandler) AddHoliday(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date        string `json:"date"` // 2006-01-02
		Description string `json:"description"`
		IsRecurring bool   `json:"isRecurring"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json body")
		return
	}

	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_date", "invalid date format, use YYYY-MM-DD")
		return
	}

	holiday, err := h.service.AddHoliday(r.Context(), date, req.Description, req.IsRecurring)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, "common.error.internal", err.Error())
		return
	}

	writeResultWithRequest(w, r, holiday, nil)
}
