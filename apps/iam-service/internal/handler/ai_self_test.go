package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

type aiSelfReaderFunc func(context.Context, string, string) (*domain.UserContext, error)

func (f aiSelfReaderFunc) GetUserContextByIDForTenant(ctx context.Context, userID, tenantID string) (*domain.UserContext, error) {
	return f(ctx, userID, tenantID)
}

func TestAISelfDisplayScopesAndRedacts(t *testing.T) {
	req := httptest.NewRequest("GET", "/internal/ai/me/display?user_id=other&tenant_id=other", nil)
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-Permissions", "ai.assistant.use")
	w := httptest.NewRecorder()
	serveAISelfDisplay(w, req, aiSelfReaderFunc(func(_ context.Context, userID, tenantID string) (*domain.UserContext, error) {
		if userID != "user-1" || tenantID != "tenant-1" {
			t.Fatalf("scope overridden: %s %s", userID, tenantID)
		}
		return &domain.UserContext{
			UserID: userID, ActiveTenantID: tenantID, DisplayName: "Nguyen Van A", PhoneNumber: "private-phone",
			TenantMemberships: []domain.TenantMembership{
				{TenantID: "other", TenantName: "private-tenant", Status: "ACTIVE", TenantStatus: "ACTIVE"},
				{TenantID: tenantID, TenantCode: "ARDA", TenantName: "Arda Labs", Status: "ACTIVE", TenantStatus: "ACTIVE"},
			},
		}, nil
	}))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Result struct {
			Tenant map[string]any `json:"tenant"`
			User   map[string]any `json:"user"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.Tenant["name"] != "Arda Labs" || response.Result.Tenant["code"] != "ARDA" || response.Result.User["name"] != "Nguyen Van A" {
		t.Fatalf("missing labels: %s", w.Body.String())
	}
	for _, private := range []string{"private-phone", "private-tenant", "tenantMemberships", "phoneNumber"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("leaked %s", private)
		}
	}
}

func TestAISelfDisplayRejectsUnverifiedScope(t *testing.T) {
	for _, missing := range []string{"X-Auth-Checked", "X-User-Id", "X-Tenant-Id", "X-Permissions"} {
		t.Run(missing, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/internal/ai/me/display", nil)
			for key, value := range map[string]string{"X-Auth-Checked": "true", "X-User-Id": "u", "X-Tenant-Id": "t", "X-Permissions": "ai.assistant.use"} {
				req.Header.Set(key, value)
			}
			req.Header.Del(missing)
			w := httptest.NewRecorder()
			serveAISelfDisplay(w, req, aiSelfReaderFunc(func(context.Context, string, string) (*domain.UserContext, error) {
				t.Fatal("unverified request reached reader")
				return nil, nil
			}))
			if w.Code != 403 {
				t.Fatalf("status %d", w.Code)
			}
		})
	}
}

func TestAISelfDisplayRejectsInvalidMembership(t *testing.T) {
	for _, scenario := range []string{"error", "other-user", "other-tenant", "inactive", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/internal/ai/me/display", nil)
			for key, value := range map[string]string{"X-Auth-Checked": "true", "X-User-Id": "u", "X-Tenant-Id": "t", "X-Permissions": "ai.assistant.use"} {
				req.Header.Set(key, value)
			}
			w := httptest.NewRecorder()
			serveAISelfDisplay(w, req, aiSelfReaderFunc(func(context.Context, string, string) (*domain.UserContext, error) {
				profile := &domain.UserContext{UserID: "u", ActiveTenantID: "t", TenantMemberships: []domain.TenantMembership{{TenantID: "t", Status: "ACTIVE", TenantStatus: "ACTIVE"}}}
				switch scenario {
				case "error":
					return nil, errors.New("database unavailable")
				case "other-user":
					profile.UserID = "other"
				case "other-tenant":
					profile.ActiveTenantID = "other"
				case "inactive":
					profile.TenantMemberships[0].Status = "INACTIVE"
				case "missing":
					profile.TenantMemberships = nil
				}
				return profile, nil
			}))
			if w.Code != 403 {
				t.Fatalf("status %d", w.Code)
			}
		})
	}
}
