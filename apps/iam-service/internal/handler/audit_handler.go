package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardaexport "github.com/arda-labs/arda/libs/go/arda-export"
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
	tenantID := firstNonEmpty(r.URL.Query().Get("tenant_id"), r.URL.Query().Get("tenantId"))
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
		respondAdminRequestError(w, r, http.StatusInternalServerError, ardaerrors.CodeInternal, err.Error())
		return
	}

	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listQuery.Page, listQuery.PerPage, total, events))
}

// ExportAudit handles direct streaming export of audit logs in XLSX or CSV format.
func (h *AuditHandler) ExportAudit(w http.ResponseWriter, r *http.Request) {
	listQuery := ardahttp.ParseListQuery(r.URL.Query())
	eventTypes := r.URL.Query()["event_type"]
	subject := r.URL.Query().Get("subject")
	result := r.URL.Query().Get("result")
	tenantID := firstNonEmpty(r.URL.Query().Get("tenant_id"), r.URL.Query().Get("tenantId"))
	sort := firstNonEmpty(listQuery.Sort, r.URL.Query().Get("sort"))
	formatStr := r.URL.Query().Get("format")

	var from, to time.Time
	if f := r.URL.Query().Get("from"); f != "" {
		from, _ = time.Parse(time.RFC3339, f)
	}
	if t := r.URL.Query().Get("to"); t != "" {
		to, _ = time.Parse(time.RFC3339, t)
	}

	format := ardaexport.NormalizeFormat(formatStr)
	filename := fmt.Sprintf("audit_export_%s", time.Now().Format("20060102_150405"))

	cols := []ardaexport.Column{
		{Header: "ID sự kiện", Key: "eventId", Type: ardaexport.CellTypeCode},
		{Header: "Loại sự kiện", Key: "eventType", Type: ardaexport.CellTypeString},
		{Header: "Chủ thể (Người dùng)", Key: "subject", Type: ardaexport.CellTypeString},
		{Header: "Hành động", Key: "action", Type: ardaexport.CellTypeString},
		{Header: "Tài nguyên", Key: "resource", Type: ardaexport.CellTypeString},
		{
			Header: "Kết quả",
			Key:    "result",
			Type:   ardaexport.CellTypeString,
			Formatter: func(v any) any {
				if s, ok := v.(string); ok {
					if s == "success" {
						return "Thành công"
					}
					return "Thất bại"
				}
				return v
			},
		},
		{Header: "IP Người dùng", Key: "clientIp", Type: ardaexport.CellTypeCode},
		{Header: "User Agent", Key: "userAgent", Type: ardaexport.CellTypeString},
		{Header: "Dịch vụ", Key: "serviceName", Type: ardaexport.CellTypeString},
		{Header: "Thời gian", Key: "timestamp", Type: ardaexport.CellTypeDate},
	}

	opts := ardaexport.StreamOptions{
		Title:     "BÁO CÁO NHẬT KÝ KIỂM TOÁN HỆ THỐNG",
		SheetName: "AuditLogs",
		Columns:   cols,
		Locale:    "vi-VN",
	}

	err := ardaexport.ServeStreamHTTP(w, r, format, filename, func(ctx context.Context, out io.Writer) error {
		rows, err := h.svc.StreamAudit(ctx, repository.QueryParams{
			EventTypes: eventTypes,
			Subject:    subject,
			Result:     result,
			TenantID:   tenantID,
			From:       from,
			To:         to,
			Sort:       sort,
		})
		if err != nil {
			return err
		}
		defer rows.Close()

		supplier := func() ([]any, error) {
			if !rows.Next() {
				if rows.Err() != nil {
					return nil, rows.Err()
				}
				return nil, io.EOF
			}
			var eventID, eventType, sub, act, res, resStatus, clientIP, userAgent, serviceName string
			var ts time.Time
			if err := rows.Scan(&eventID, &eventType, &sub, &act, &res, &resStatus, &clientIP, &userAgent, &serviceName, &ts); err != nil {
				return nil, err
			}
			return []any{eventID, eventType, sub, act, res, resStatus, clientIP, userAgent, serviceName, ts}, nil
		}

		if format == ardaexport.FormatCSV {
			return ardaexport.StreamCSV(ctx, out, opts, supplier)
		}
		return ardaexport.StreamXLSX(ctx, out, opts, supplier)
	})

	if err != nil {
		respondAdminRequestError(w, r, http.StatusInternalServerError, ardaerrors.CodeInternal, err.Error())
	}
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
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondAdminJSON(w, r, http.StatusOK, stats)
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
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondAdminJSON(w, r, http.StatusOK, result)
}
