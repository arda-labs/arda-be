package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/mfa"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
)

const (
	testSecret = "0482913789abcdef0482913789abcdef"
	knownUser  = "11111111-1111-1111-1111-111111111111"
)

type fakeUsers struct{ ctx *domain.UserContext }

func (f *fakeUsers) lookup(id string) (*domain.UserContext, error) {
	if id == knownUser || id == "subject-1" || id == "kratos-1" {
		return f.ctx, nil
	}
	if id == "boom" {
		return nil, errors.New("connection refused")
	}
	return nil, service.ErrUserNotFound
}
func (f *fakeUsers) GetUserContextBySubject(_ context.Context, s string) (*domain.UserContext, error) {
	return f.lookup(s)
}
func (f *fakeUsers) GetUserContextByID(_ context.Context, id string) (*domain.UserContext, error) {
	return f.lookup(id)
}
func (f *fakeUsers) GetUserContextByIDForTenant(_ context.Context, id, tenantID string) (*domain.UserContext, error) {
	if tenantID != "tenant-a" {
		return nil, service.ErrTenantMembershipInactive
	}
	uc, err := f.lookup(id)
	if err != nil {
		return nil, err
	}
	scoped := *uc
	scoped.ActiveTenantID = tenantID
	return &scoped, nil
}
func (f *fakeUsers) GetUserContextByKratosIdentityID(_ context.Context, id string) (*domain.UserContext, error) {
	return f.lookup(id)
}
func (f *fakeUsers) ResolveOrLinkKratosIdentity(_ context.Context, id, email, _ string) (*domain.UserContext, error) {
	if id == "kratos-1" || email == "u1@example.com" {
		return f.ctx, nil
	}
	return nil, service.ErrUserNotFound
}
func (f *fakeUsers) ResolveOrLinkIdentity(_ context.Context, providerID, externalID, _, _ string, verified bool) (*domain.UserContext, error) {
	switch {
	case providerID == "google" && externalID == "ext-1":
		return f.ctx, nil
	case providerID == "google" && externalID == "inactive":
		return nil, service.ErrIdentityMappingInactive
	case !verified:
		return nil, service.ErrVerifiedEmailRequired
	}
	return nil, service.ErrUserNotFound
}

type fakeSessions struct {
	created  []string
	revoked  []string
	touched  []string
	trusted  []string
	limitHit bool
}

func (f *fakeSessions) GetOrCreateDevice(_ context.Context, userID, _, _, _, _, _, _ string) (*domain.Device, error) {
	return &domain.Device{ID: "dev-" + userID[:4], UserID: userID}, nil
}
func (f *fakeSessions) TrustDevice(_ context.Context, deviceID, _ string, _ bool) error {
	f.trusted = append(f.trusted, deviceID)
	return nil
}
func (f *fakeSessions) CreateSession(_ context.Context, userID, deviceID, _, _, _, _, _ string) (*domain.Session, error) {
	if f.limitHit {
		return nil, service.ErrSessionLimitReached
	}
	f.created = append(f.created, userID+"/"+deviceID)
	return &domain.Session{ID: "sess-1", UserID: userID, DeviceID: deviceID, ExpiresAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}, nil
}
func (f *fakeSessions) ForceRevokeSession(_ context.Context, sessionID, _ string) error {
	f.revoked = append(f.revoked, sessionID)
	return nil
}
func (f *fakeSessions) TouchSession(_ context.Context, sessionID string) error {
	f.touched = append(f.touched, sessionID)
	return nil
}

type fakeMFA struct{ enrolled map[string]bool }

func (f *fakeMFA) GetSettings(_ context.Context, userID string) (*domain.MFASettings, error) {
	if f.enrolled[userID] {
		return &domain.MFASettings{UserID: userID, Method: "totp", IsEnrolled: true}, nil
	}
	return nil, nil
}
func (f *fakeMFA) CheckMFA(_ context.Context, userID, deviceToken string) (*service.MFAResult, error) {
	if !f.enrolled[userID] || deviceToken == "trusted" {
		return &service.MFAResult{Methods: []string{"totp"}}, nil
	}
	return &service.MFAResult{RequiresMFA: true, CanUseMFA: true, Methods: []string{"totp"}}, nil
}
func (f *fakeMFA) VerifyCode(_ context.Context, userID, code string) error {
	switch {
	case !f.enrolled[userID]:
		return service.ErrMFANotEnrolled
	case code == "000000":
		return service.ErrMFALocked
	case code != "482913":
		return service.ErrMFAInvalidCode
	}
	return nil
}
func (f *fakeMFA) GenerateSecret(_ context.Context, userID, _, _ string) (*mfa.TOTPSecret, error) {
	return &mfa.TOTPSecret{Secret: "S3CRET", OTPAuth: "otpauth://totp/arda:" + userID}, nil
}
func (f *fakeMFA) VerifyAndEnroll(_ context.Context, userID, code string) ([]string, error) {
	if f.enrolled[userID] {
		return nil, service.ErrMFAAlreadyEnrolled
	}
	if code != "482913" {
		return nil, service.ErrMFAInvalidEnrollmentCode
	}
	f.enrolled[userID] = true
	return []string{"aaaa-bbbb", "cccc-dddd"}, nil
}

type fakeAudit struct{ events []*domain.AuthEvent }

func (f *fakeAudit) Event(_ context.Context, e *domain.AuthEvent) { f.events = append(f.events, e) }

type fixture struct {
	client   iamv1.GatewayServiceClient
	sessions *fakeSessions
	mfa      *fakeMFA
	audit    *fakeAudit
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	sessions := &fakeSessions{}
	mfaSvc := &fakeMFA{enrolled: map[string]bool{}}
	auditLog := &fakeAudit{}
	users := &fakeUsers{ctx: &domain.UserContext{
		UserID: knownUser, Subject: "subject-1", Username: "u1", Email: "u1@example.com", Status: "ACTIVE",
		TenantID: "tenant-a", ActiveTenantID: "tenant-a", AuthVersion: 7,
		TenantMemberships: []domain.TenantMembership{{TenantID: "tenant-a", TenantCode: "A", Status: "ACTIVE", IsDefault: true}},
		OrgIDs:            []string{"org-1"}, GroupIDs: []string{}, Roles: []string{"ADMIN"}, Permissions: []string{"iam.user.read"},
		GlobalCapabilitiesLoaded: true,
	}}
	srv := newServer(testSecret, nil, Dependencies{Users: users, Sessions: sessions, MFA: mfaSvc, Audit: auditLog})
	listener := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &fixture{client: iamv1.NewGatewayServiceClient(conn), sessions: sessions, mfa: mfaSvc, audit: auditLog}
}

func asSource(t *testing.T, source string) context.Context {
	t.Helper()
	token, err := identity.Issue(testSecret, source, "iam-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return metadata.AppendToOutgoingContext(context.Background(), identity.MetadataKey, token, "x-request-id", "req-1")
}

func gatewayCtx(t *testing.T) context.Context { return asSource(t, "auth-gateway") }

func wantCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if status.Code(err) != want {
		t.Fatalf("status = %v (%v), want %v", status.Code(err), err, want)
	}
}

func TestGatewayServiceIsAuthGatewayOnly(t *testing.T) {
	f := newFixture(t)
	_, err := f.client.Ready(context.Background(), &iamv1.ReadyRequest{})
	wantCode(t, err, codes.Unauthenticated)
	_, err = f.client.Ready(asSource(t, "workflow-service"), &iamv1.ReadyRequest{})
	wantCode(t, err, codes.PermissionDenied)
	_, err = f.client.GetUserBySubject(asSource(t, "notification-service"), &iamv1.GetUserBySubjectRequest{Subject: "subject-1"})
	wantCode(t, err, codes.PermissionDenied)
	if _, err := f.client.Ready(gatewayCtx(t), &iamv1.ReadyRequest{}); err != nil {
		t.Fatalf("Ready from auth-gateway: %v", err)
	}
}

func TestUserContextLookups(t *testing.T) {
	f := newFixture(t)
	ctx := gatewayCtx(t)

	resp, err := f.client.GetUserBySubject(ctx, &iamv1.GetUserBySubjectRequest{Subject: "subject-1"})
	if err != nil {
		t.Fatal(err)
	}
	u := resp.GetUser()
	if u.GetUserId() != knownUser || u.GetAuthVersion() != 7 || u.GetStatus() != "ACTIVE" || len(u.GetTenantMemberships()) != 1 ||
		u.GetTenantMemberships()[0].GetTenantCode() != "A" || u.GetRoles()[0] != "ADMIN" || !u.GetGlobalCapabilitiesLoaded() {
		t.Fatalf("user context not mapped: %+v", u)
	}
	_, err = f.client.GetUserBySubject(ctx, &iamv1.GetUserBySubjectRequest{Subject: "ghost"})
	wantCode(t, err, codes.NotFound)
	_, err = f.client.GetUserBySubject(ctx, &iamv1.GetUserBySubjectRequest{Subject: "boom"})
	wantCode(t, err, codes.Internal)
	_, err = f.client.GetUserBySubject(ctx, &iamv1.GetUserBySubjectRequest{})
	wantCode(t, err, codes.InvalidArgument)

	byID, err := f.client.GetUserById(ctx, &iamv1.GetUserByIdRequest{UserId: knownUser})
	if err != nil || byID.GetUser().GetActiveTenantId() != "tenant-a" {
		t.Fatalf("GetUserById = %v, %v", byID, err)
	}
	_, err = f.client.GetUserById(ctx, &iamv1.GetUserByIdRequest{UserId: "ghost"})
	wantCode(t, err, codes.NotFound)
	_, err = f.client.GetUserById(ctx, &iamv1.GetUserByIdRequest{UserId: knownUser, TenantId: "tenant-b"})
	wantCode(t, err, codes.PermissionDenied)
	scoped, err := f.client.GetUserById(ctx, &iamv1.GetUserByIdRequest{UserId: knownUser, TenantId: "tenant-a"})
	if err != nil || scoped.GetUser().GetActiveTenantId() != "tenant-a" {
		t.Fatalf("tenant-scoped GetUserById = %v, %v", scoped, err)
	}

	if _, err := f.client.GetUserByKratosIdentityId(ctx, &iamv1.GetUserByKratosIdentityIdRequest{IdentityId: "kratos-1"}); err != nil {
		t.Fatal(err)
	}
	_, err = f.client.GetUserByKratosIdentityId(ctx, &iamv1.GetUserByKratosIdentityIdRequest{IdentityId: "ghost"})
	wantCode(t, err, codes.NotFound)
}

func TestIdentityResolveOrLink(t *testing.T) {
	f := newFixture(t)
	ctx := gatewayCtx(t)

	if _, err := f.client.ResolveOrLinkKratosIdentity(ctx, &iamv1.ResolveOrLinkKratosIdentityRequest{IdentityId: "new", Email: "u1@example.com"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.client.ResolveOrLinkKratosIdentity(ctx, &iamv1.ResolveOrLinkKratosIdentityRequest{IdentityId: "new", Email: "nobody@example.com"})
	wantCode(t, err, codes.NotFound)
	_, err = f.client.ResolveOrLinkKratosIdentity(ctx, &iamv1.ResolveOrLinkKratosIdentityRequest{})
	wantCode(t, err, codes.InvalidArgument)

	if _, err := f.client.ResolveOrLinkIdentity(ctx, &iamv1.ResolveOrLinkIdentityRequest{ProviderId: "google", ExternalId: "ext-1"}); err != nil {
		t.Fatal(err)
	}
	_, err = f.client.ResolveOrLinkIdentity(ctx, &iamv1.ResolveOrLinkIdentityRequest{ProviderId: "google", ExternalId: "inactive"})
	wantCode(t, err, codes.FailedPrecondition)
	_, err = f.client.ResolveOrLinkIdentity(ctx, &iamv1.ResolveOrLinkIdentityRequest{ProviderId: "google", ExternalId: "new", Email: "x@example.com"})
	wantCode(t, err, codes.FailedPrecondition)
	_, err = f.client.ResolveOrLinkIdentity(ctx, &iamv1.ResolveOrLinkIdentityRequest{ProviderId: "google"})
	wantCode(t, err, codes.InvalidArgument)
}

func TestSessionLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := gatewayCtx(t)

	created, err := f.client.CreateSession(ctx, &iamv1.CreateSessionRequest{
		UserId: knownUser, IpAddress: "203.0.113.9", UserAgent: "Mozilla/5.0", DeviceName: "Chrome on Windows",
		DeviceToken: "tok", TrustForMfa: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetSessionId() != "sess-1" || created.GetDeviceId() != "dev-1111" || created.GetExpiresAt().AsTime().Year() != 2026 {
		t.Fatalf("CreateSession = %+v", created)
	}
	if len(f.sessions.trusted) != 1 || f.sessions.trusted[0] != "dev-1111" {
		t.Fatalf("trust_for_mfa not applied: %v", f.sessions.trusted)
	}
	if len(f.audit.events) != 1 || f.audit.events[0].EventType != "session_created" || f.audit.events[0].ClientIP != "203.0.113.9" ||
		f.audit.events[0].UserAgent != "Mozilla/5.0" || f.audit.events[0].RequestID != "req-1" || f.audit.events[0].Subject != knownUser {
		t.Fatalf("session_created audit = %+v", f.audit.events)
	}

	f.sessions.limitHit = true
	_, err = f.client.CreateSession(ctx, &iamv1.CreateSessionRequest{UserId: knownUser})
	wantCode(t, err, codes.ResourceExhausted)
	f.sessions.limitHit = false
	_, err = f.client.CreateSession(ctx, &iamv1.CreateSessionRequest{})
	wantCode(t, err, codes.InvalidArgument)

	if _, err := f.client.RevokeSession(ctx, &iamv1.RevokeSessionRequest{SessionId: "sess-1"}); err != nil {
		t.Fatal(err)
	}
	if len(f.sessions.revoked) != 1 || f.audit.events[1].EventType != "session_revoked" || f.audit.events[1].UserAgent != "grpc:auth-gateway" {
		t.Fatalf("revoke not recorded: %v %+v", f.sessions.revoked, f.audit.events)
	}
	_, err = f.client.RevokeSession(ctx, &iamv1.RevokeSessionRequest{})
	wantCode(t, err, codes.InvalidArgument)

	if _, err := f.client.TouchSession(ctx, &iamv1.TouchSessionRequest{SessionId: "sess-1"}); err != nil {
		t.Fatal(err)
	}
	if len(f.sessions.touched) != 1 {
		t.Fatalf("touch not recorded: %v", f.sessions.touched)
	}
}

func TestMFAFlow(t *testing.T) {
	f := newFixture(t)
	ctx := gatewayCtx(t)

	st, err := f.client.GetMfaStatus(ctx, &iamv1.GetMfaStatusRequest{UserId: knownUser})
	if err != nil || st.GetIsEnrolled() {
		t.Fatalf("status before enrollment = %v, %v", st, err)
	}
	_, err = f.client.VerifyMfa(ctx, &iamv1.VerifyMfaRequest{UserId: knownUser, Code: "482913"})
	wantCode(t, err, codes.FailedPrecondition) // not enrolled

	secret, err := f.client.GenerateMfaSecret(ctx, &iamv1.GenerateMfaSecretRequest{UserId: knownUser, Username: "u1", Email: "u1@example.com"})
	if err != nil || secret.GetSecret() != "S3CRET" || secret.GetOtpauthUrl() == "" {
		t.Fatalf("GenerateMfaSecret = %v, %v", secret, err)
	}
	_, err = f.client.VerifyMfaEnrollment(ctx, &iamv1.VerifyMfaEnrollmentRequest{UserId: knownUser, Code: "999999"})
	wantCode(t, err, codes.InvalidArgument)
	enrolled, err := f.client.VerifyMfaEnrollment(ctx, &iamv1.VerifyMfaEnrollmentRequest{UserId: knownUser, Code: "482913"})
	if err != nil || len(enrolled.GetBackupCodes()) != 2 {
		t.Fatalf("VerifyMfaEnrollment = %v, %v", enrolled, err)
	}
	_, err = f.client.VerifyMfaEnrollment(ctx, &iamv1.VerifyMfaEnrollmentRequest{UserId: knownUser, Code: "482913"})
	wantCode(t, err, codes.FailedPrecondition) // already enrolled

	st, err = f.client.GetMfaStatus(ctx, &iamv1.GetMfaStatusRequest{UserId: knownUser})
	if err != nil || !st.GetIsEnrolled() || st.GetMethod() != "totp" {
		t.Fatalf("status after enrollment = %v, %v", st, err)
	}

	check, err := f.client.CheckMfa(ctx, &iamv1.CheckMfaRequest{UserId: knownUser, DeviceToken: "unknown"})
	if err != nil || !check.GetRequiresMfa() || !check.GetCanUseMfa() || check.GetMethods()[0] != "totp" {
		t.Fatalf("CheckMfa untrusted = %v, %v", check, err)
	}
	check, err = f.client.CheckMfa(ctx, &iamv1.CheckMfaRequest{UserId: knownUser, DeviceToken: "trusted"})
	if err != nil || check.GetRequiresMfa() {
		t.Fatalf("CheckMfa trusted = %v, %v", check, err)
	}

	if _, err := f.client.VerifyMfa(ctx, &iamv1.VerifyMfaRequest{UserId: knownUser, Code: "482913"}); err != nil {
		t.Fatal(err)
	}
	_, err = f.client.VerifyMfa(ctx, &iamv1.VerifyMfaRequest{UserId: knownUser, Code: "654321"})
	wantCode(t, err, codes.InvalidArgument)
	if status.Convert(err).Message() != "invalid MFA code" {
		t.Fatalf("wrong code message = %q", status.Convert(err).Message())
	}
	_, err = f.client.VerifyMfa(ctx, &iamv1.VerifyMfaRequest{UserId: knownUser, Code: "000000"})
	wantCode(t, err, codes.FailedPrecondition) // locked
	_, err = f.client.VerifyMfa(ctx, &iamv1.VerifyMfaRequest{UserId: knownUser})
	wantCode(t, err, codes.InvalidArgument)
}

func TestSourcePolicyCoversEveryGatewayMethod(t *testing.T) {
	_, methodSources := sourcePolicy()
	for _, m := range iamv1.GatewayService_ServiceDesc.Methods {
		full := "/arda.iam.v1.GatewayService/" + m.MethodName
		sources, ok := methodSources[full]
		if !ok || len(sources) != 1 {
			t.Fatalf("%s: expected exactly one allowed source, got %v", full, sources)
		}
		if _, ok := sources[gatewaySource]; !ok {
			t.Fatalf("%s: auth-gateway not allowed", full)
		}
	}
}
