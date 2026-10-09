package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
)

func TestAgreementDailySnapshotInternalRouteValidatesDateAndTenant(t *testing.T) {
	snapshot := handler.NewAgreementDailySnapshotHandler(
		service.NewAgreementDailySnapshotService(repository.NewLoanRepository(nil)),
	)
	router := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, snapshot, nil)

	request := httptest.NewRequest(http.MethodPost, "/internal/jobs/agreement-daily-snapshot?to_date=not-a-date", nil)
	request.Header.Set("X-Tenant-Id", "tenant-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid date status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/internal/jobs/agreement-daily-snapshot?to_date=2026-10-08", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing tenant status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}
