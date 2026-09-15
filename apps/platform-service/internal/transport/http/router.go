package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func NewRouter(platformHandler *handler.PlatformHandler, calendarHandler *handler.CalendarHandler, menuHandler *handler.MenuHandler, eodHandler *handler.EODHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))

	// DB-driven navigation (MFE shell sidebar)
	mux.HandleFunc("/api/platform/menus/effective", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, r)
			return
		}
		menuHandler.GetEffectiveMenu(w, r)
	})
	mux.HandleFunc("/api/platform/menus", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			menuHandler.ListMenuItems(w, r)
		case http.MethodPost, http.MethodPut:
			menuHandler.UpsertMenuItem(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/platform/menus/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, r)
			return
		}
		menuHandler.DeleteMenuItem(w, r)
	})

	mux.HandleFunc("/api/platform/eod/run", eodHandler.RunCOB)
	mux.HandleFunc("/api/platform/eod/seed", eodHandler.SeedCOBJobs)
	mux.HandleFunc("GET /api/platform/jobs", eodHandler.ListJobs)
	mux.HandleFunc("GET /api/platform/jobs/runs", eodHandler.ListJobRuns)
	mux.HandleFunc("/api/platform/working-hours", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListWorkingHours(w, r)
		case http.MethodPost, http.MethodPut:
			platformHandler.UpsertWorkingHour(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("DELETE /api/platform/working-hours/{id}", platformHandler.DeleteWorkingHour)

	mux.HandleFunc("/api/platform/public/branding", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, r)
			return
		}
		platformHandler.GetPublicBranding(w, r)
	})

	mux.HandleFunc("/api/platform/parameters", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListParameters(w, r)
		case http.MethodPost, http.MethodPut:
			platformHandler.UpsertParameter(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/lookups", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListLookupCategories(w, r)
		case http.MethodPost, http.MethodPut:
			platformHandler.UpsertLookupCategory(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/lookups/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/values") {
			methodNotAllowed(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListLookupValues(w, r)
		case http.MethodPost:
			platformHandler.CreateLookupValue(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/parameters/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			platformHandler.DeleteParameter(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/lookups/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			platformHandler.DeleteLookupCategory(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/lookup-values/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			platformHandler.DeleteLookupValue(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/organizations", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListOrganizations(w, r)
		case http.MethodPost:
			platformHandler.CreateOrganization(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/organizations/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.GetOrganization(w, r)
		case http.MethodPut:
			platformHandler.UpdateOrganization(w, r)
		case http.MethodDelete:
			platformHandler.DeleteOrganization(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/geo/admin-units", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListGeoAdminUnits(w, r)
		case http.MethodPost, http.MethodPut:
			platformHandler.UpsertGeoAdminUnit(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/credit-institutions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListCreditInstitutions(w, r)
		case http.MethodPost:
			platformHandler.CreateCreditInstitution(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/credit-institutions/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.GetCreditInstitution(w, r)
		case http.MethodPut:
			platformHandler.UpdateCreditInstitution(w, r)
		case http.MethodDelete:
			platformHandler.DeleteCreditInstitution(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/areas", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListAreas(w, r)
		case http.MethodPost:
			platformHandler.CreateArea(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/areas/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.GetArea(w, r)
		case http.MethodPut:
			platformHandler.UpdateArea(w, r)
		case http.MethodDelete:
			platformHandler.DeleteArea(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/templates", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListFileTemplates(w, r)
		case http.MethodPost:
			platformHandler.CreateFileTemplate(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/platform/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.GetFileTemplate(w, r)
		case http.MethodPut:
			platformHandler.UpdateFileTemplate(w, r)
		case http.MethodDelete:
			platformHandler.DeleteFileTemplate(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	// ── Calendar & Cut-off ──
	mux.HandleFunc("/api/platform/calendar/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, r)
			return
		}
		calendarHandler.GetStatus(w, r)
	})
	mux.HandleFunc("/api/platform/calendar/eod", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, r)
			return
		}
		calendarHandler.TriggerEOD(w, r)
	})
	mux.HandleFunc("/api/platform/calendar/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, r)
			return
		}
		calendarHandler.EvaluateDate(w, r)
	})
	mux.HandleFunc("/api/platform/calendar/holidays", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			calendarHandler.ListHolidays(w, r)
		case http.MethodPost:
			calendarHandler.AddHoliday(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Tenant scoping applies
	// inside the handlers via the X-Tenant-Id guard (see InternalAIListOrganizations).
	mux.Handle("GET /internal/ai/organizations", internalAIService(http.HandlerFunc(platformHandler.InternalAIListOrganizations)))
	mux.Handle("GET /internal/ai/parameters", internalAIService(http.HandlerFunc(platformHandler.InternalAIListParameters)))
	mux.Handle("GET /internal/ai/lookups/{lookupCode}/values", internalAIService(http.HandlerFunc(platformHandler.InternalAILookupValues)))
	mux.Handle("GET /internal/ai/calendar/status", internalAIService(http.HandlerFunc(calendarHandler.InternalAICalendarStatus)))

	return mux
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id, X-User-Id, ...) is forwarded by the caller, not trusted from
// browsers — this route is never exposed to them.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ardahttp.WriteProblem(w, r, http.StatusServiceUnavailable, ardaerrors.New(ardaerrors.CodeInternal, "internal service identity is not configured"))
		})
	}
	return identity.RequireServiceAuth(secret, "platform-service", identity.AllowedSources("ai-service"))(next)
}

func health(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	}
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}
