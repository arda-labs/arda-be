package ardahttp

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

func TestDeriveErrorCode(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		want    string
	}{
		{"bad request json", http.StatusBadRequest, "invalid body JSON", ardaerrors.CodeInvalidJSON},
		{"bad request required", http.StatusBadRequest, "tenant_id is required", ardaerrors.CodeRequired},
		{"bad request other", http.StatusBadRequest, "too short", ardaerrors.CodeInvalidInput},
		{"unauthorized", http.StatusUnauthorized, "missing header", ardaerrors.CodeUnauthorized},
		{"upstream down", http.StatusServiceUnavailable, "dependency timeout", ardaerrors.CodeBadGateway},
		{"bad gateway", http.StatusBadGateway, "workflow", ardaerrors.CodeBadGateway},
		{"internal", http.StatusInternalServerError, "panic", ardaerrors.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveErrorCode(tt.status, tt.message); got != tt.want {
				t.Errorf("DeriveErrorCode(%d, %q) = %q, want %q", tt.status, tt.message, got, tt.want)
			}
		})
	}
}

func TestDeriveStatusFromMessage(t *testing.T) {
	tests := []struct {
		message string
		want    int
	}{
		{"customer not found", http.StatusNotFound},
		{"code already exists conflict", http.StatusConflict},
		{"account cannot be closed", http.StatusConflict},
		{"workflow rejected the request", http.StatusBadGateway},
		{"unexpected failure", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		if got := DeriveStatusFromMessage(tt.message); got != tt.want {
			t.Errorf("DeriveStatusFromMessage(%q) = %d, want %d", tt.message, got, tt.want)
		}
	}
}

func TestWriteServiceErrorNilIsNoOp(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	WriteServiceError(recorder, request, nil)
	if recorder.Body.Len() != 0 {
		t.Fatalf("nil error must not write a body, got %q", recorder.Body.String())
	}
}

func TestWriteServiceErrorTypedUsesCodeStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	err := ardaerrors.New(ardaerrors.CodeSessionLimitReached, "too many sessions")
	WriteServiceError(recorder, request, err)

	body := recorder.Body.String()
	if recorder.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if !strings.Contains(body, ardaerrors.CodeSessionLimitReached) {
		t.Errorf("body missing code: %s", body)
	}
	if !strings.Contains(body, "request_id") {
		t.Errorf("body missing request_id: %s", body)
	}
}

func TestWriteServiceErrorSQLNoRows(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	WriteServiceError(recorder, request, sql.ErrNoRows)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if !strings.Contains(recorder.Body.String(), ardaerrors.CodeNotFound) {
		t.Errorf("body missing not_found code: %s", recorder.Body.String())
	}
}

func TestWriteServiceErrorUntypedLegacyMappings(t *testing.T) {
	tests := []struct {
		message string
		status  int
	}{
		{"role not found", http.StatusNotFound},
		{"duplicate membership conflict", http.StatusConflict},
		{"workflow unavailable, try again", http.StatusBadGateway},
		{"database exploded", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		WriteServiceError(recorder, request, errors.New(tt.message))
		if recorder.Code != tt.status {
			t.Errorf("%q: status = %d, want %d", tt.message, recorder.Code, tt.status)
		}
	}
}

func TestWriteServiceErrorWrapsCauseChain(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	typed := ardaerrors.Wrap(ardaerrors.CodeConflict, "position cycle detected", errors.New("depth exceeded"))
	WriteServiceError(recorder, request, typed)

	if recorder.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if !strings.Contains(recorder.Body.String(), ardaerrors.CodeConflict) {
		t.Errorf("body missing conflict code: %s", recorder.Body.String())
	}
}