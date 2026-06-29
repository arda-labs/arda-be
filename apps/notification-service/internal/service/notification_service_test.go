package service

import (
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
