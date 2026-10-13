package iamclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamgrpc "github.com/arda-labs/arda/libs/go/arda-grpc/client/iam"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
)

// gatewayAPI is the subset of iamgrpc.GatewayClient the adapter uses; tests
// substitute an in-memory connection through iamgrpc.NewGatewayClientWithConn.
type gatewayAPI interface {
	Ready(ctx context.Context) error
	GetUserBySubject(ctx context.Context, subject string) (*iamv1.UserContext, error)
	GetUserByID(ctx context.Context, userID, tenantID string) (*iamv1.UserContext, error)
	GetUserByKratosIdentityID(ctx context.Context, identityID string) (*iamv1.UserContext, error)
	ResolveOrLinkKratosIdentity(ctx context.Context, identityID, email, name string) (*iamv1.UserContext, error)
	ResolveOrLinkIdentity(ctx context.Context, providerID, externalID, email, name string, emailVerified bool) (*iamv1.UserContext, error)
	CreateSession(ctx context.Context, req *iamv1.CreateSessionRequest) (*iamv1.CreateSessionResponse, error)
	RevokeSession(ctx context.Context, sessionID string) error
	TouchSession(ctx context.Context, sessionID string) error
	GetMFAStatus(ctx context.Context, userID string) (*iamv1.GetMfaStatusResponse, error)
	CheckMFA(ctx context.Context, userID, deviceToken string) (*iamv1.CheckMfaResponse, error)
	VerifyMFA(ctx context.Context, userID, code string) error
	GenerateMFASecret(ctx context.Context, userID, username, email string) (*iamv1.GenerateMfaSecretResponse, error)
	VerifyMFAEnrollment(ctx context.Context, userID, code string) (*iamv1.VerifyMfaEnrollmentResponse, error)
	Close() error
}

// GRPCClient adapts arda.iam.v1.GatewayService to the Client interface the
// handlers were written against, so switching transports changes no handler.
type GRPCClient struct {
	api           gatewayAPI
	serviceSecret string
}

// NewGRPC dials IAM's gRPC endpoint with mTLS and the auth-gateway workload
// identity (ARDA_SERVICE_AUTH_SECRET, ARDA_GRPC_* from the environment).
func NewGRPC(addr string, serviceSecret string) (*GRPCClient, error) {
	api, err := iamgrpc.DialGateway(addr, "auth-gateway")
	if err != nil {
		return nil, err
	}
	return &GRPCClient{api: api, serviceSecret: serviceSecret}, nil
}

// NewGRPCWithAPI wraps an already-connected gateway client (tests).
func NewGRPCWithAPI(api gatewayAPI, serviceSecret string) *GRPCClient {
	return &GRPCClient{api: api, serviceSecret: serviceSecret}
}

// ValidateServiceIdentity mirrors HTTPClient: production must not start with
// an unauthenticated auth-gateway -> IAM adapter.
func (c *GRPCClient) ValidateServiceIdentity() error {
	if c.serviceSecret == "" {
		return fmt.Errorf("auth-gateway service identity secret is required")
	}
	if _, err := identity.Issue(c.serviceSecret, "auth-gateway", "iam-service", time.Now(), time.Minute); err != nil {
		return fmt.Errorf("auth-gateway service identity is invalid: %w", err)
	}
	return nil
}

// Close releases the gRPC connection.
func (c *GRPCClient) Close() error {
	return c.api.Close()
}

func (c *GRPCClient) Ready(ctx context.Context) error {
	if err := c.api.Ready(ctx); err != nil {
		return fmt.Errorf("iam ready request failed: %w", err)
	}
	return nil
}

func (c *GRPCClient) GetUserBySubject(ctx context.Context, subject string) (*UserContext, error) {
	return userContextResult(c.api.GetUserBySubject(ctx, subject))
}

func (c *GRPCClient) GetUserByID(ctx context.Context, id string, tenantID ...string) (*UserContext, error) {
	scope := ""
	if len(tenantID) > 0 {
		scope = tenantID[0]
	}
	return userContextResult(c.api.GetUserByID(ctx, id, scope))
}

func (c *GRPCClient) GetUserByKratosIdentityID(ctx context.Context, identityID string) (*UserContext, error) {
	return userContextResult(c.api.GetUserByKratosIdentityID(ctx, identityID))
}

func (c *GRPCClient) ResolveOrLinkKratosIdentity(ctx context.Context, identityID, email, name string) (*UserContext, error) {
	return userContextResult(c.api.ResolveOrLinkKratosIdentity(ctx, identityID, email, name))
}

func (c *GRPCClient) ResolveOrLinkIdentity(ctx context.Context, providerID, externalID, email, name string, emailVerified bool) (*UserContext, error) {
	return userContextResult(c.api.ResolveOrLinkIdentity(ctx, providerID, externalID, email, name, emailVerified))
}

// userContextResult keeps the HTTP adapter's contract: NOT_FOUND becomes
// ErrUserNotFound (definitive), every other failure stays a transport error.
func userContextResult(uc *iamv1.UserContext, err error) (*UserContext, error) {
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("%w: %s", ErrUserNotFound, status.Convert(err).Message())
		}
		return nil, fmt.Errorf("iam request failed: %w", err)
	}
	if uc == nil {
		return nil, fmt.Errorf("%w: empty response", ErrUserNotFound)
	}
	return fromProtoUserContext(uc), nil
}

func (c *GRPCClient) CreateSession(ctx context.Context, req *CreateSessionRequest) (*CreateSessionResponse, error) {
	if req == nil {
		return nil, errors.New("create session request is nil")
	}
	resp, err := c.api.CreateSession(ctx, &iamv1.CreateSessionRequest{
		UserId:          req.UserID,
		HydraSessionId:  req.HydraSessionID,
		AccessTokenJti:  req.AccessTokenJTI,
		RefreshTokenJti: req.RefreshTokenJTI,
		IpAddress:       req.IPAddress,
		UserAgent:       req.UserAgent,
		DeviceName:      req.DeviceName,
		DeviceType:      req.DeviceType,
		Os:              req.OS,
		Browser:         req.Browser,
		Fingerprint:     req.Fingerprint,
		DeviceToken:     req.DeviceToken,
		TrustForMfa:     req.TrustForMFA,
	})
	if err != nil {
		if status.Code(err) == codes.ResourceExhausted {
			return nil, fmt.Errorf("session limit reached: %s", status.Convert(err).Message())
		}
		return nil, fmt.Errorf("create session request: %w", err)
	}
	out := &CreateSessionResponse{SessionID: resp.GetSessionId(), DeviceID: resp.GetDeviceId()}
	if resp.GetExpiresAt() != nil {
		out.ExpiresAt = resp.GetExpiresAt().AsTime()
	}
	return out, nil
}

func (c *GRPCClient) RevokeSession(ctx context.Context, sessionID string) error {
	if err := c.api.RevokeSession(ctx, sessionID); err != nil {
		return fmt.Errorf("revoke session request: %w", err)
	}
	return nil
}

func (c *GRPCClient) TouchSession(ctx context.Context, sessionID string) error {
	if err := c.api.TouchSession(ctx, sessionID); err != nil {
		return fmt.Errorf("touch session request: %w", err)
	}
	return nil
}

func (c *GRPCClient) GetMFAStatus(ctx context.Context, userID string) (*MFAStatus, error) {
	resp, err := c.api.GetMFAStatus(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("mfa status request: %w", err)
	}
	return &MFAStatus{IsEnrolled: resp.GetIsEnrolled(), Method: resp.GetMethod()}, nil
}

func (c *GRPCClient) CheckMFA(ctx context.Context, userID, deviceToken string) (*MFACheck, error) {
	resp, err := c.api.CheckMFA(ctx, userID, deviceToken)
	if err != nil {
		return nil, fmt.Errorf("mfa device check request: %w", err)
	}
	return &MFACheck{RequiresMFA: resp.GetRequiresMfa(), CanUseMFA: resp.GetCanUseMfa(), Methods: resp.GetMethods()}, nil
}

func (c *GRPCClient) VerifyMFA(ctx context.Context, userID, code string) error {
	if err := c.api.VerifyMFA(ctx, userID, code); err != nil {
		return fmt.Errorf("mfa verify failed: %w", err)
	}
	return nil
}

func (c *GRPCClient) GenerateMFASecret(ctx context.Context, userID, username, email string) (*MFASecret, error) {
	resp, err := c.api.GenerateMFASecret(ctx, userID, username, email)
	if err != nil {
		return nil, fmt.Errorf("mfa enroll request: %w", err)
	}
	return &MFASecret{Secret: resp.GetSecret(), OTPAuthURL: resp.GetOtpauthUrl()}, nil
}

func (c *GRPCClient) VerifyMFAEnrollment(ctx context.Context, userID, code string) (*MFAEnrollResponse, error) {
	resp, err := c.api.VerifyMFAEnrollment(ctx, userID, code)
	if err != nil {
		return nil, fmt.Errorf("mfa verify enroll failed: %w", err)
	}
	return &MFAEnrollResponse{Status: "enrolled", BackupCodes: resp.GetBackupCodes()}, nil
}

// fromProtoUserContext mirrors JSON decoding of the HTTP contract, where IAM
// emits roles/permissions as "[]": every repeated field maps to a non-nil
// slice so callers see the same shape from both transports.
func fromProtoUserContext(uc *iamv1.UserContext) *UserContext {
	out := &UserContext{
		UserID:                   uc.GetUserId(),
		Subject:                  uc.GetSubject(),
		Username:                 uc.GetUsername(),
		Email:                    uc.GetEmail(),
		DisplayName:              uc.GetDisplayName(),
		Nickname:                 uc.GetNickname(),
		FirstName:                uc.GetFirstName(),
		LastName:                 uc.GetLastName(),
		PhoneNumber:              uc.GetPhoneNumber(),
		Birthdate:                uc.GetBirthdate(),
		Gender:                   uc.GetGender(),
		Address:                  uc.GetAddress(),
		Country:                  uc.GetCountry(),
		Status:                   uc.GetStatus(),
		PictureURL:               uc.GetPictureUrl(),
		AvatarFileID:             uc.GetAvatarFileId(),
		CoverImageURL:            uc.GetCoverImageUrl(),
		CoverFileID:              uc.GetCoverFileId(),
		TenantID:                 uc.GetTenantId(),
		ActiveTenantID:           uc.GetActiveTenantId(),
		TenantMemberships:        make([]TenantMembership, 0, len(uc.GetTenantMemberships())),
		TenantSelectionRequired:  uc.GetTenantSelectionRequired(),
		OrgIDs:                   nonNil(uc.GetOrgIds()),
		GroupIDs:                 nonNil(uc.GetGroupIds()),
		Roles:                    nonNil(uc.GetRoles()),
		Permissions:              nonNil(uc.GetPermissions()),
		GlobalRoles:              nonNil(uc.GetGlobalRoles()),
		GlobalPermissions:        nonNil(uc.GetGlobalPermissions()),
		IsGlobalAdmin:            uc.GetIsGlobalAdmin(),
		GlobalCapabilitiesLoaded: uc.GetGlobalCapabilitiesLoaded(),
		AuthVersion:              uc.GetAuthVersion(),
		Timezone:                 uc.GetTimezone(),
		Locale:                   uc.GetLocale(),
	}
	for _, m := range uc.GetTenantMemberships() {
		out.TenantMemberships = append(out.TenantMemberships, TenantMembership{
			TenantID:     m.GetTenantId(),
			TenantCode:   m.GetTenantCode(),
			TenantName:   m.GetTenantName(),
			TenantStatus: m.GetTenantStatus(),
			Status:       m.GetStatus(),
			IsDefault:    m.GetIsDefault(),
		})
	}
	return out
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
