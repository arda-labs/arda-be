package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

func decodeJSON[T any](w http.ResponseWriter, r *http.Request, target *T) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid body")
		return false
	}
	return true
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, err error) {
	writeErrorCode(w, r, status, ardaerrors.CodeForStatus(status), err.Error())
}

func writeItem(w http.ResponseWriter, r *http.Request, item any) {
	writeJSON(w, r, http.StatusCreated, item)
}

// ProjectHandler exposes project + risk-info endpoints (P1c.4).
type ProjectHandler struct {
	repo *repository.ProjectRepository
}

func NewProjectHandler(repo *repository.ProjectRepository) *ProjectHandler {
	return &ProjectHandler{repo: repo}
}

// ListProjectTypes handles GET /api/crm/project-types.
func (h *ProjectHandler) ListProjectTypes(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	items, err := h.repo.ListTypes(r.Context(), scope.TenantID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeListAll(w, r, items)
}

// UpsertProjectType handles POST/PUT /api/crm/project-types.
func (h *ProjectHandler) UpsertProjectType(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	var in repository.ProjectType
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		writeProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, "code and name are required"))
		return
	}
	in.TenantID = scope.TenantID
	in.IsActive = true
	in.CreatedBy = scope.UserID
	created, err := h.repo.UpsertType(r.Context(), &in)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, err)
		return
	}
	writeItem(w, r, created)
}

// ListProjects handles GET /api/crm/projects.
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	items, err := h.repo.ListProjects(r.Context(), scope.TenantID, scope.OrgIDs,
		r.URL.Query().Get("status"), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeListAll(w, r, items)
}

// CreateProject handles POST /api/crm/projects.
func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	var in repository.Project
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.ProjectCode) == "" || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.TypeCode) == "" {
		writeProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, "project_code, name and type_code are required"))
		return
	}
	in.TenantID = scope.TenantID
	in.Status = "ACTIVE"
	in.OrgCode = scope.ResolveOrgID()
	in.CreatedBy = scope.UserID
	created, err := h.repo.CreateProject(r.Context(), &in)
	if err != nil {
		writeProblem(w, r, http.StatusConflict, err)
		return
	}
	writeItem(w, r, created)
}

// ListRiskFlags handles GET /api/crm/customers/{id}/risk-flags.
func (h *ProjectHandler) ListRiskFlags(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	items, err := h.repo.ListRiskFlags(r.Context(), scope.TenantID, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeListAll(w, r, items)
}

// AddRiskFlag handles POST /api/crm/customers/{id}/risk-flags.
func (h *ProjectHandler) AddRiskFlag(w http.ResponseWriter, r *http.Request) {
	scope := ScopeFromRequest(r)
	var in repository.RiskFlag
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.FlagCode) == "" || strings.TrimSpace(in.EffectiveDate) == "" {
		writeProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, "flag_code and effective_date are required"))
		return
	}
	in.TenantID = scope.TenantID
	in.CustomerID = r.PathValue("id")
	in.IsActive = true
	in.CreatedBy = scope.UserID
	created, err := h.repo.AddRiskFlag(r.Context(), &in)
	if err != nil {
		writeProblem(w, r, http.StatusConflict, err)
		return
	}
	writeItem(w, r, created)
}
