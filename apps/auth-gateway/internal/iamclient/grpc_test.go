package iamclient

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamgrpc "github.com/arda-labs/arda/libs/go/arda-grpc/client/iam"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
)

// fakeGateway is a GatewayService that records requests and answers with
// fixed data or the status code the test asks for.
type fakeGateway struct {
	iamv1.UnimplementedGatewayServiceServer
	lastCreate *iamv1.CreateSessionRequest
	lastByID   *iamv1.GetUserByIdRequest
	revoked    []string
	touched    []string
	verified   []string
}

func (f *fakeGateway) user() *iamv1.UserContext {
	return &iamv1.UserContext{
		UserId: "u1", Subject: "s1", Username: "alice", Email: "alice@example.com", Status: "ACTIVE",
		TenantId: "t1", ActiveTenantId: "t1", AuthVersion: 3, Timezone: "Asia/Ho_Chi_Minh",
		TenantMemberships: []*iamv1.TenantMembership{{TenantId: "t1", TenantCode: "T1", Status: "ACTIVE", IsDefault: true}},
		Roles:             []string{"ADMIN"}, Permissions: []string{"iam.user.read"}, IsGlobalAdmin: true, GlobalCapabilitiesLoaded: true,
	}
}

func (f *fakeGateway) Ready(context.Context, *iamv1.ReadyRequest) (*iamv1.ReadyResponse, error) {
	return &iamv1.ReadyResponse{}, nil
}
func (f *fakeGateway) GetUserBySubject(_ context.Context, req *iamv1.GetUserBySubjectRequest) (*iamv1.UserContextResponse, error) {
	switch req.GetSubject() {
	case "s1":
		return &iamv1.UserContextResponse{User: f.user()}, nil
	case "down":
		return nil, status.Error(codes.Unavailable, "db down")
	}
	return nil, status.Error(codes.NotFound, "user not found")
}
func (f *fakeGateway) GetUserById(_ context.Context, req *iamv1.GetUserByIdRequest) (*iamv1.UserContextResponse, error) {
	f.lastByID = req
	if req.GetTenantId() == "t2" {
		return nil, status.Error(codes.PermissionDenied, "user is not an active member of the tenant")
	}
	if req.GetUserId() != "u1" {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return &iamv1.UserContextResponse{User: f.user()}, nil
}
func (f *fakeGateway) GetUserByKratosIdentityId(_ context.Context, req *iamv1.GetUserByKratosIdentityIdRequest) (*iamv1.UserContextResponse, error) {
	if req.GetIdentityId() != "k1" {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return &iamv1.UserContextResponse{User: f.user()}, nil
}
func (f *fakeGateway) ResolveOrLinkKratosIdentity(_ context.Context, req *iamv1.ResolveOrLinkKratosIdentityRequest) (*iamv1.UserContextResponse, error) {
	if req.GetEmail() != "alice@example.com" {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return &iamv1.UserContextResponse{User: f.user()}, nil
}
func (f *fakeGateway) ResolveOrLinkIdentity(_ context.Context, req *iamv1.ResolveOrLinkIdentityRequest) (*iamv1.UserContextResponse, error) {
	if !req.GetEmailVerified() {
		return nil, status.Error(codes.FailedPrecondition, "verified email is required for first-time identity link")
	}
	return &iamv1.UserContextResponse{User: f.user()}, nil
}
func (f *fakeGateway) CreateSession(_ context.Context, req *iamv1.CreateSessionRequest) (*iamv1.CreateSessionResponse, error) {
	f.lastCreate = req
	if req.GetUserId() == "full" {
		return nil, status.Error(codes.ResourceExhausted, "maximum concurrent sessions reached (5)")
	}
	return &iamv1.CreateSessionResponse{SessionId: "sess-1", DeviceId: "dev-1", ExpiresAt: timestamppb.New(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))}, nil
}
func (f *fakeGateway) RevokeSession(_ context.Context, req *iamv1.RevokeSessionRequest) (*iamv1.RevokeSessionResponse, error) {
	f.revoked = append(f.revoked, req.GetSessionId())
	return &iamv1.RevokeSessionResponse{}, nil
}
func (f *fakeGateway) TouchSession(_ context.Context, req *iamv1.TouchSessionRequest) (*iamv1.TouchSessionResponse, error) {
	f.touched = append(f.touched, req.GetSessionId())
	return &iamv1.TouchSessionResponse{}, nil
}
func (f *fakeGateway) GetMfaStatus(context.Context, *iamv1.GetMfaStatusRequest) (*iamv1.GetMfaStatusResponse, error) {
	return &iamv1.GetMfaStatusResponse{IsEnrolled: true, Method: "totp"}, nil
}
func (f *fakeGateway) CheckMfa(_ context.Context, req *iamv1.CheckMfaRequest) (*iamv1.CheckMfaResponse, error) {
	return &iamv1.CheckMfaResponse{RequiresMfa: req.GetDeviceToken() != "trusted", CanUseMfa: true, Methods: []string{"totp"}}, nil
}
func (f *fakeGateway) VerifyMfa(_ context.Context, req *iamv1.VerifyMfaRequest) (*iamv1.VerifyMfaResponse, error) {
	f.verified = append(f.verified, req.GetUserId()+":"+req.GetCode())
	switch req.GetCode() {
	case "123456":
		return &iamv1.VerifyMfaResponse{}, nil
	case "000000":
		return nil, status.Error(codes.FailedPrecondition, "too many failed MFA attempts; try again later")
	}
	return nil, status.Error(codes.InvalidArgument, "invalid MFA code")
}
func (f *fakeGateway) GenerateMfaSecret(_ context.Context, req *iamv1.GenerateMfaSecretRequest) (*iamv1.GenerateMfaSecretResponse, error) {
	return &iamv1.GenerateMfaSecretResponse{Secret: "S3CRET", OtpauthUrl: "otpauth://totp/arda:" + req.GetUsername()}, nil
}
func (f *fakeGateway) VerifyMfaEnrollment(_ context.Context, req *iamv1.VerifyMfaEnrollmentRequest) (*iamv1.VerifyMfaEnrollmentResponse, error) {
	if req.GetCode() != "123456" {
		return nil, status.Error(codes.InvalidArgument, "invalid TOTP code")
	}
	return &iamv1.VerifyMfaEnrollmentResponse{BackupCodes: []string{"a", "b"}}, nil
}

func newTestClient(t *testing.T) (*GRPCClient, *fakeGateway) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	fake := &fakeGateway{}
	iamv1.RegisterGatewayServiceServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewGRPCWithAPI(iamgrpc.NewGatewayClientWithConn(conn), strings.Repeat("s", 32)), fake
}

func TestGRPCUserContextMapping(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	uc, err := client.GetUserBySubject(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if uc.UserID != "u1" || uc.AuthVersion != 3 || uc.Status != "ACTIVE" || uc.Timezone != "Asia/Ho_Chi_Minh" ||
		!uc.IsGlobalAdmin || !uc.GlobalCapabilitiesLoaded || len(uc.TenantMemberships) != 1 || uc.TenantMemberships[0].TenantCode != "T1" {
		t.Fatalf("user context = %+v", uc)
	}
	if uc.OrgIDs == nil || uc.GroupIDs == nil || uc.GlobalRoles == nil || uc.GlobalPermissions == nil {
		t.Fatalf("repeated fields must decode to non-nil slices: %+v", uc)
	}

	_, err = client.GetUserBySubject(ctx, "ghost")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("NOT_FOUND must map to ErrUserNotFound, got %v", err)
	}
	_, err = client.GetUserBySubject(ctx, "down")
	if err == nil || errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UNAVAILABLE must stay a transport error, got %v", err)
	}
}

func TestGRPCGetUserByIDPassesTenantScope(t *testing.T) {
	client, fake := newTestClient(t)
	ctx := context.Background()

	if _, err := client.GetUserByID(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if fake.lastByID.GetTenantId() != "" {
		t.Fatalf("tenant scope leaked: %q", fake.lastByID.GetTenantId())
	}
	if _, err := client.GetUserByID(ctx, "u1", "t1"); err != nil {
		t.Fatal(err)
	}
	if fake.lastByID.GetTenantId() != "t1" {
		t.Fatalf("tenant scope not sent: %q", fake.lastByID.GetTenantId())
	}
	_, err := client.GetUserByID(ctx, "u1", "t2")
	if err == nil || errors.Is(err, ErrUserNotFound) {
		t.Fatalf("PERMISSION_DENIED must be a non-not-found error, got %v", err)
	}
	if _, err := client.GetUserByKratosIdentityID(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResolveOrLinkKratosIdentity(ctx, "k9", "alice@example.com", "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResolveOrLinkIdentity(ctx, "google", "x", "alice@example.com", "Alice", false); err == nil {
		t.Fatal("FAILED_PRECONDITION must surface as an error")
	}
}

func TestGRPCSessionMapping(t *testing.T) {
	client, fake := newTestClient(t)
	ctx := context.Background()

	resp, err := client.CreateSession(ctx, &CreateSessionRequest{
		UserID: "u1", IPAddress: "203.0.113.9", UserAgent: "ua", DeviceName: "Chrome on Windows", DeviceType: "browser",
		OS: "Windows", Browser: "Chrome", Fingerprint: "fp", DeviceToken: "tok", TrustForMFA: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SessionID != "sess-1" || resp.DeviceID != "dev-1" || resp.ExpiresAt.Year() != 2026 {
		t.Fatalf("create session response = %+v", resp)
	}
	if fake.lastCreate.GetDeviceName() != "Chrome on Windows" || fake.lastCreate.GetOs() != "Windows" || !fake.lastCreate.GetTrustForMfa() ||
		fake.lastCreate.GetFingerprint() != "fp" || fake.lastCreate.GetIpAddress() != "203.0.113.9" {
		t.Fatalf("create session request not mapped: %+v", fake.lastCreate)
	}
	_, err = client.CreateSession(ctx, &CreateSessionRequest{UserID: "full"})
	if err == nil || !strings.HasPrefix(err.Error(), "session limit reached") {
		t.Fatalf("RESOURCE_EXHAUSTED must keep the session-limit message, got %v", err)
	}
	if err := client.RevokeSession(ctx, "sess-1"); err != nil || len(fake.revoked) != 1 {
		t.Fatalf("revoke = %v %v", err, fake.revoked)
	}
	if err := client.TouchSession(ctx, "sess-1"); err != nil || len(fake.touched) != 1 {
		t.Fatalf("touch = %v %v", err, fake.touched)
	}
}

func TestGRPCMFAMapping(t *testing.T) {
	client, fake := newTestClient(t)
	ctx := context.Background()

	st, err := client.GetMFAStatus(ctx, "u1")
	if err != nil || !st.IsEnrolled || st.Method != "totp" {
		t.Fatalf("mfa status = %+v %v", st, err)
	}
	check, err := client.CheckMFA(ctx, "u1", "unknown")
	if err != nil || !check.RequiresMFA || !check.CanUseMFA || check.Methods[0] != "totp" {
		t.Fatalf("check mfa = %+v %v", check, err)
	}
	if check, _ := client.CheckMFA(ctx, "u1", "trusted"); check.RequiresMFA {
		t.Fatal("trusted device must not require MFA")
	}
	if err := client.VerifyMFA(ctx, "u1", "123456"); err != nil {
		t.Fatal(err)
	}
	if err := client.VerifyMFA(ctx, "u1", "999999"); err == nil {
		t.Fatal("wrong code must fail")
	}
	if err := client.VerifyMFA(ctx, "u1", "000000"); err == nil {
		t.Fatal("locked account must fail")
	}
	if len(fake.verified) != 3 || fake.verified[0] != "u1:123456" {
		t.Fatalf("verify calls = %v", fake.verified)
	}
	secret, err := client.GenerateMFASecret(ctx, "u1", "alice", "alice@example.com")
	if err != nil || secret.Secret != "S3CRET" || !strings.HasSuffix(secret.OTPAuthURL, "alice") {
		t.Fatalf("secret = %+v %v", secret, err)
	}
	enroll, err := client.VerifyMFAEnrollment(ctx, "u1", "123456")
	if err != nil || enroll.Status != "enrolled" || len(enroll.BackupCodes) != 2 {
		t.Fatalf("enroll = %+v %v", enroll, err)
	}
	if _, err := client.VerifyMFAEnrollment(ctx, "u1", "1"); err == nil {
		t.Fatal("wrong enrollment code must fail")
	}
}

func TestGRPCValidateServiceIdentity(t *testing.T) {
	if err := NewGRPCWithAPI(nil, "").ValidateServiceIdentity(); err == nil {
		t.Fatal("empty secret accepted")
	}
	if err := NewGRPCWithAPI(nil, strings.Repeat("s", 32)).ValidateServiceIdentity(); err != nil {
		t.Fatalf("valid secret rejected: %v", err)
	}
	if _, err := NewGRPC("", strings.Repeat("s", 32)); err == nil {
		t.Fatal("empty address accepted")
	}
}
