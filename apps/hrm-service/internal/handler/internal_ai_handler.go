package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/hrm-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// aiEmployee is the redacted employee shape exposed to the AI SDK. Internal
// linkage fields (tenant_id, iam_user_id, org/position/job-title refs) are
// dropped here; the response allowlist in contracts/ai-internal/hrm-v1.json
// drops them again as defense in depth.
type aiEmployee struct {
	ID           string `json:"id"`
	EmployeeCode string `json:"employeeCode"`
	FullName     string `json:"fullName"`
	Status       string `json:"status"`
}

func toAIEmployees(items []domain.Employee) []aiEmployee {
	redacted := make([]aiEmployee, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiEmployee{
			ID:           item.ID,
			EmployeeCode: item.EmployeeCode,
			FullName:     item.FullName,
			Status:       item.Status,
		})
	}
	return redacted
}

// InternalAIListEmployees serves GET /internal/ai/employees for ai-service.
// The signed caller assertion and delegated subject headers are verified by
// the router's internalAIService middleware before reaching this handler;
// tenant scoping happens again in the repository layer via
// ardametadata.FromOutgoing, so a tenant can never see another tenant's
// employees.
func (h *HRMHandler) InternalAIListEmployees(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(strings.ToUpper(r.URL.Query().Get("status")))
	items, err := h.repo.ListEmployees(r.Context(), search)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if status != "" {
		filtered := make([]domain.Employee, 0, len(items))
		for _, item := range items {
			if strings.EqualFold(item.Status, status) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeListAll(w, r, toAIEmployees(items))
}
