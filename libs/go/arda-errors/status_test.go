package ardaerrors

import (
	"net/http"
	"testing"
)

func TestStatusForCode(t *testing.T) {
	tests := []struct {
		code   string
		status int
	}{
		{CodeInvalidJSON, http.StatusBadRequest},
		{CodeRequired, http.StatusBadRequest},
		{CodeInvalidInput, http.StatusBadRequest},
		{CodeUnauthorized, http.StatusUnauthorized},
		{CodeUserContextRequired, http.StatusUnauthorized},
		{CodeForbidden, http.StatusForbidden},
		{CodeNotFound, http.StatusNotFound},
		{CodeUserNotFound, http.StatusNotFound},
		{CodeConflict, http.StatusConflict},
		{CodeSuperAdminLastActive, http.StatusConflict},
		{CodeSessionLimitReached, http.StatusConflict},
		{CodeMethodNotAllowed, http.StatusMethodNotAllowed},
		{CodeBadGateway, http.StatusBadGateway},
		{CodeInternal, http.StatusInternalServerError},
		{"totally.unknown.code", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		if got := StatusForCode(tt.code); got != tt.status {
			t.Errorf("StatusForCode(%q) = %d, want %d", tt.code, got, tt.status)
		}
	}
}