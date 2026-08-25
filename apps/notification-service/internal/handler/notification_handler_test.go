package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/notification-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func TestWriteNotificationErrorDoesNotMisclassifyTenantAsUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	req.Header.Set(ardahttp.HeaderRequestID, "req-notification-test")
	rec := httptest.NewRecorder()

	writeNotificationError(rec, req, service.ErrTenantMigrationRequired)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	var problem ardahttp.Problem
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != ardaerrors.CodeTenantMigrationRequired {
		t.Fatalf("code = %q, want %q", problem.Code, ardaerrors.CodeTenantMigrationRequired)
	}
	if problem.Status != http.StatusConflict {
		t.Fatalf("problem status = %d, want %d", problem.Status, http.StatusConflict)
	}
	if errors.Is(service.ErrTenantMigrationRequired, service.ErrTenantScopeRequired) {
		t.Fatal("tenant migration and missing scope errors must remain distinct")
	}
}
