package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/domain"
	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	"github.com/arda-labs/arda/apps/crm-service/internal/service"
)

// MemberHandler serves the QTDND membership surface: the member register and
// the capital-movement request pipeline (maker-checker).
type MemberHandler struct {
	svc memberService
}

type memberService interface {
	ListMembers(ctx context.Context, p repository.ListMembersParams) ([]domain.Member, int, error)
	GetMember(ctx context.Context, tenantID, id string) (*domain.Member, error)
	RegisterMember(ctx context.Context, tenantID string, in service.RegisterMemberInput) (*domain.Member, error)
	UpdateMemberProfile(ctx context.Context, tenantID, id, bookNo, typeCode, status, actor string, version int64) (*domain.Member, error)
	SubmitCapitalRequest(ctx context.Context, tenantID string, in service.SubmitCapitalRequestInput) (*domain.MemberRequest, error)
	ListMemberRequests(ctx context.Context, tenantID, memberID, status string) ([]domain.MemberRequest, error)
	ResolveCapitalRequest(ctx context.Context, tenantID, id, decision, actor string, dataVersion int64) (*domain.MemberRequest, *domain.Member, error)
	ListMembersForReporting(ctx context.Context, tenantID, orgCode string) ([]domain.Member, error)
	ListMemberRequestsForReporting(ctx context.Context, tenantID string) ([]repository.MemberCapitalRequestRow, error)
}

func NewMemberHandler(svc memberService) *MemberHandler {
	return &MemberHandler{svc: svc}
}

// Members handles GET (list) and POST (register) /api/crm/members.
func (h *MemberHandler) Members(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeError(w, r, http.StatusForbidden, "tenant scope is required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, total, err := h.svc.ListMembers(r.Context(), repository.ListMembersParams{
			TenantID: tenantID,
			OrgCodes: orgScope(r),
			Status:   strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
			TypeCode: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("member_type_code"))),
			Q:        strings.TrimSpace(r.URL.Query().Get("q")),
			Sort:     r.URL.Query().Get("sort"),
			Order:    r.URL.Query().Get("order"),
		})
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"items": items, "total": total,
		})
	case http.MethodPost:
		var in service.RegisterMemberInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		in.Actor = r.Header.Get("X-User-Id")
		if in.OrgCode == "" {
			in.OrgCode = r.Header.Get("X-Org-Id")
		}
		created, err := h.svc.RegisterMember(r.Context(), tenantID, in)
		if err != nil {
			status := http.StatusBadRequest
			if err == repository.ErrMemberConflict {
				status = http.StatusConflict
			}
			writeError(w, r, status, err.Error())
			return
		}
		writeJSON(w, r, http.StatusCreated, created)
	default:
		writeMethodNotAllowed(w, r)
	}
}

// MemberByID handles GET and PUT /api/crm/members/{id}.
func (h *MemberHandler) MemberByID(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeError(w, r, http.StatusForbidden, "tenant scope is required")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, r, http.StatusNotFound, "member not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		m, err := h.svc.GetMember(r.Context(), tenantID, id)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if m == nil {
			writeError(w, r, http.StatusNotFound, "member not found")
			return
		}
		writeJSON(w, r, http.StatusOK, m)
	case http.MethodPut:
		var in struct {
			MemberBookNo   string `json:"member_book_no"`
			MemberTypeCode string `json:"member_type_code"`
			MemberStatus   string `json:"member_status"`
			Version        int64  `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		updated, err := h.svc.UpdateMemberProfile(r.Context(), tenantID, id,
			in.MemberBookNo, in.MemberTypeCode, in.MemberStatus, r.Header.Get("X-User-Id"), in.Version)
		if err != nil {
			status := http.StatusBadRequest
			if err == repository.ErrMemberVersionConflict {
				status = http.StatusConflict
			}
			writeError(w, r, status, err.Error())
			return
		}
		writeJSON(w, r, http.StatusOK, updated)
	default:
		writeMethodNotAllowed(w, r)
	}
}

// MemberRequests handles GET (pipeline) and POST (stage a capital movement).
func (h *MemberHandler) MemberRequests(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeError(w, r, http.StatusForbidden, "tenant scope is required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.svc.ListMemberRequests(r.Context(), tenantID,
			strings.TrimSpace(r.URL.Query().Get("member_id")),
			strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": len(items)})
	case http.MethodPost:
		var in service.SubmitCapitalRequestInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		in.RequestType = strings.ToUpper(strings.TrimSpace(in.RequestType))
		in.Actor = r.Header.Get("X-User-Id")
		created, err := h.svc.SubmitCapitalRequest(r.Context(), tenantID, in)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, r, http.StatusCreated, created)
	default:
		writeMethodNotAllowed(w, r)
	}
}

// MemberRequestDecision handles POST /api/crm/member-requests/{id}/decision.
func (h *MemberHandler) MemberRequestDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeError(w, r, http.StatusForbidden, "tenant scope is required")
		return
	}
	id := r.PathValue("id")
	var in struct {
		Decision    string `json:"decision"`
		DataVersion int64  `json:"data_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	req, member, err := h.svc.ResolveCapitalRequest(r.Context(), tenantID, id,
		strings.ToUpper(strings.TrimSpace(in.Decision)), r.Header.Get("X-User-Id"), in.DataVersion)
	if err != nil {
		status := http.StatusBadRequest
		if err == repository.ErrMemberVersionConflict {
			status = http.StatusConflict
		}
		writeError(w, r, status, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"request": req, "member": member})
}

// orgScope reads the delegated org scope headers (X-Org-Id / X-User-Org-Ids),
// de-duplicated and order-preserving.
func orgScope(r *http.Request) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(r.Header.Get("X-Org-Id"))
	for _, part := range strings.Split(r.Header.Get("X-User-Org-Ids"), ",") {
		add(part)
	}
	return out
}
