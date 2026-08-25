package service

import (
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
)

func TestValidateAcceptRequiresUserIDForInApp(t *testing.T) {
	err := validateAccept(AcceptInput{
		TenantID:       "tenant_1",
		IdempotencyKey: "key_1",
		TemplateKey:    "approval.requested",
		Channels:       []string{domain.ChannelInApp},
		Recipients:     []domain.Recipient{{Type: "email", Address: "a@example.com"}},
	})
	if err == nil {
		t.Fatal("expected in_app recipient validation error")
	}
}

func TestNotificationKeyFallback(t *testing.T) {
	got := notificationKey("", "approval.requested", "title")
	if got != "notifications:approval.requested.title" {
		t.Fatalf("unexpected key: %s", got)
	}
}

func TestValidateTenantIDClassifiesLegacyAndMissingScopes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{name: "missing", in: "", want: ErrTenantScopeRequired},
		{name: "legacy default", in: "default", want: ErrTenantMigrationRequired},
		{name: "legacy default case insensitive", in: " DEFAULT ", want: ErrTenantMigrationRequired},
		{name: "valid", in: "tenant-acme", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateTenantID(tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("validateTenantID(%q) = %v, want %v", tt.in, err, tt.want)
			}
		})
	}
}

func TestValidateUserContextRequiresAuthenticatedUser(t *testing.T) {
	if err := validateUserContext("tenant-acme", ""); !errors.Is(err, ErrUserContextRequired) {
		t.Fatalf("validateUserContext missing user = %v, want %v", err, ErrUserContextRequired)
	}
	if err := validateUserContext("default", "user-1"); !errors.Is(err, ErrTenantMigrationRequired) {
		t.Fatalf("validateUserContext legacy tenant = %v, want %v", err, ErrTenantMigrationRequired)
	}
}
