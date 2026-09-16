package service

import (
	"context"
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/push"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
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

// TestSubscribePushRejectsSSRFEndpoints drives the service with a nil repo: an
// unsafe endpoint must be rejected before any repository call.
func TestSubscribePushRejectsSSRFEndpoints(t *testing.T) {
	svc := NewNotificationService(nil, nil)
	for _, endpoint := range []string{
		"http://example.com/push",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.1/push",
		"https://127.0.0.1/push",
		"https://[::1]/push",
		"https://100.64.0.1/push",
		"https://localhost/push",
	} {
		in := PushSubscribeInput{Endpoint: endpoint}
		in.Keys.P256dh = "p256dh"
		in.Keys.Auth = "auth"
		if err := subscribePushSafely(t, svc, in); err == nil {
			t.Errorf("SubscribePush(%q) = nil, want validation error", endpoint)
		}
	}
}

func TestSubscribePushValidatesEndpointBeforeRepository(t *testing.T) {
	svc := NewNotificationService(nil, nil)
	in := PushSubscribeInput{Endpoint: "https://fcm.googleapis.com/fcm/send/abc"}
	in.Keys.P256dh = "p256dh"
	in.Keys.Auth = "auth"
	// A nil repository proves validation passed: the call panics only after it.
	defer func() {
		if recover() == nil {
			t.Fatal("expected the nil repository to be reached for a valid endpoint")
		}
	}()
	_ = svc.SubscribePush(context.Background(), "tenant-acme", "user-1", "agent", in)
}

func subscribePushSafely(t *testing.T, svc *NotificationService, in PushSubscribeInput) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SubscribePush(%q) panicked instead of rejecting the endpoint: %v", in.Endpoint, r)
		}
	}()
	return svc.SubscribePush(context.Background(), "tenant-acme", "user-1", "agent", in)
}

func TestPushEndpointAcceptsProviderHost(t *testing.T) {
	if err := push.ValidateEndpoint("https://updates.push.services.mozilla.com/wpush/v2/xyz"); err != nil {
		t.Fatalf("ValidateEndpoint(public provider) = %v, want nil", err)
	}
}

// TestUpsertSenderRejectsUnsafeHost drives the service with a nil repo: a host
// that could reach the cluster, metadata endpoint or loopback must be rejected
// before any repository call.
func TestUpsertSenderRejectsUnsafeHost(t *testing.T) {
	svc := NewNotificationService(nil, nil)
	for _, host := range []string{
		"127.0.0.1",
		"10.0.0.5",
		"172.16.3.4",
		"192.168.1.20",
		"169.254.169.254",
		"100.64.0.1",
		"fe80::1",
		"localhost",
		"smtp.internal",
		"instance-data",
		"  ",
	} {
		_, err := svc.UpsertSender(context.Background(), "tenant-acme", "user-1", &repository.SenderConfig{
			Host:        host,
			FromAddress: "ops@example.com",
		})
		if err == nil {
			t.Errorf("UpsertSender(%q) = nil, want validation error", host)
		}
	}
}

// TestUpsertSenderForcesTLSForPublicHost proves a public host passes validation
// and that the stored config always requests TLS, even when the request did not.
func TestUpsertSenderForcesTLSForPublicHost(t *testing.T) {
	svc := NewNotificationService(nil, nil)
	in := &repository.SenderConfig{Host: "smtp.gmail.com", FromAddress: "ops@example.com"}
	defer func() {
		if recover() == nil {
			t.Fatal("expected the nil repository to be reached for a valid host")
		}
		if !in.UseTLS {
			t.Fatal("UpsertSender must persist use_tls=true (STARTTLS is mandatory)")
		}
	}()
	_, _ = svc.UpsertSender(context.Background(), "tenant-acme", "user-1", in)
}
