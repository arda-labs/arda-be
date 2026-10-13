package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/mfa"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
)

// gatewaySource is the only workload allowed to call GatewayService.
const gatewaySource = "auth-gateway"

// UserContexts is the slice of UserService the gateway surface needs.
type UserContexts interface {
	GetUserContextBySubject(ctx context.Context, subject string) (*domain.UserContext, error)
	GetUserContextByID(ctx context.Context, id string) (*domain.UserContext, error)
	GetUserContextByIDForTenant(ctx context.Context, id, tenantID string) (*domain.UserContext, error)
	GetUserContextByKratosIdentityID(ctx context.Context, identityID string) (*domain.UserContext, error)
	ResolveOrLinkKratosIdentity(ctx context.Context, identityID, email, name string) (*domain.UserContext, error)
	ResolveOrLinkIdentity(ctx context.Context, providerID, externalID, email, name string, emailVerified bool) (*domain.UserContext, error)
}

// Sessions is the slice of SessionService the gateway surface needs.
type Sessions interface {
	GetOrCreateDevice(ctx context.Context, userID, deviceName, deviceType, os, browser, fingerprint, deviceToken string) (*domain.Device, error)
	TrustDevice(ctx context.Context, deviceID, userID string, trusted bool) error
	CreateSession(ctx context.Context, userID, deviceID, hydraID, accessJTI, refreshJTI, ip, userAgent string) (*domain.Session, error)
	ForceRevokeSession(ctx context.Context, sessionID, reason string) error
	TouchSession(ctx context.Context, sessionID string) error
}

// MFA is the slice of MFAService the gateway surface needs.
type MFA interface {
	GetSettings(ctx context.Context, userID string) (*domain.MFASettings, error)
	CheckMFA(ctx context.Context, userID, deviceToken string) (*service.MFAResult, error)
	VerifyCode(ctx context.Context, userID, code string) error
	GenerateSecret(ctx context.Context, userID, username, email string) (*mfa.TOTPSecret, error)
	VerifyAndEnroll(ctx context.Context, userID, code string) ([]string, error)
}

// Audit records session lifecycle events; *audit.Logger satisfies it.
type Audit interface {
	Event(ctx context.Context, e *domain.AuthEvent)
}

// GatewayServer serves arda.iam.v1.GatewayService for auth-gateway on top of
// the same service layer as the HTTP handlers.
type GatewayServer struct {
	iamv1.UnimplementedGatewayServiceServer
	users    UserContexts
	sessions Sessions
	mfa      MFA
	audit    Audit
	logger   *slog.Logger
}

// NewGatewayServer wires the gateway surface; audit may be nil.
func NewGatewayServer(users UserContexts, sessions Sessions, mfaSvc MFA, auditLogger Audit) *GatewayServer {
	return &GatewayServer{users: users, sessions: sessions, mfa: mfaSvc, audit: auditLogger, logger: slog.Default()}
}

func (s *GatewayServer) Ready(context.Context, *iamv1.ReadyRequest) (*iamv1.ReadyResponse, error) {
	return &iamv1.ReadyResponse{}, nil
}

func (s *GatewayServer) GetUserBySubject(ctx context.Context, req *iamv1.GetUserBySubjectRequest) (*iamv1.UserContextResponse, error) {
	subject := strings.TrimSpace(req.GetSubject())
	if subject == "" {
		return nil, status.Error(codes.InvalidArgument, "subject is required")
	}
	uc, err := s.users.GetUserContextBySubject(ctx, subject)
	return s.userContextResponse(uc, err)
}

func (s *GatewayServer) GetUserById(ctx context.Context, req *iamv1.GetUserByIdRequest) (*iamv1.UserContextResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if tenantID := strings.TrimSpace(req.GetTenantId()); tenantID != "" {
		uc, err := s.users.GetUserContextByIDForTenant(ctx, userID, tenantID)
		return s.userContextResponse(uc, err)
	}
	uc, err := s.users.GetUserContextByID(ctx, userID)
	return s.userContextResponse(uc, err)
}

func (s *GatewayServer) GetUserByKratosIdentityId(ctx context.Context, req *iamv1.GetUserByKratosIdentityIdRequest) (*iamv1.UserContextResponse, error) {
	identityID := strings.TrimSpace(req.GetIdentityId())
	if identityID == "" {
		return nil, status.Error(codes.InvalidArgument, "identity_id is required")
	}
	uc, err := s.users.GetUserContextByKratosIdentityID(ctx, identityID)
	return s.userContextResponse(uc, err)
}

func (s *GatewayServer) ResolveOrLinkKratosIdentity(ctx context.Context, req *iamv1.ResolveOrLinkKratosIdentityRequest) (*iamv1.UserContextResponse, error) {
	identityID := strings.TrimSpace(req.GetIdentityId())
	if identityID == "" {
		return nil, status.Error(codes.InvalidArgument, "identity_id is required")
	}
	uc, err := s.users.ResolveOrLinkKratosIdentity(ctx, identityID, strings.TrimSpace(req.GetEmail()), strings.TrimSpace(req.GetName()))
	return s.userContextResponse(uc, err)
}

func (s *GatewayServer) ResolveOrLinkIdentity(ctx context.Context, req *iamv1.ResolveOrLinkIdentityRequest) (*iamv1.UserContextResponse, error) {
	providerID := strings.TrimSpace(req.GetProviderId())
	externalID := strings.TrimSpace(req.GetExternalId())
	if providerID == "" || externalID == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_id and external_id are required")
	}
	uc, err := s.users.ResolveOrLinkIdentity(ctx, providerID, externalID, strings.TrimSpace(req.GetEmail()), strings.TrimSpace(req.GetName()), req.GetEmailVerified())
	return s.userContextResponse(uc, err)
}

func (s *GatewayServer) userContextResponse(uc *domain.UserContext, err error) (*iamv1.UserContextResponse, error) {
	if err != nil {
		return nil, s.mapUserError(err)
	}
	if uc == nil {
		return nil, status.Error(codes.NotFound, service.ErrUserNotFound.Error())
	}
	return &iamv1.UserContextResponse{User: toProtoUserContext(uc)}, nil
}

// mapUserError keeps "not found" definitive and everything else transient:
// the gateway revokes a session only on NOT_FOUND, so a database outage must
// never surface as one.
func (s *GatewayServer) mapUserError(err error) error {
	switch {
	case errors.Is(err, service.ErrUserNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, service.ErrTenantMembershipInactive):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, service.ErrIdentityMappingInactive), errors.Is(err, service.ErrVerifiedEmailRequired):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		s.logger.Error("gateway user context lookup failed", "err", err)
		return status.Error(codes.Internal, "user context lookup failed")
	}
}

func (s *GatewayServer) CreateSession(ctx context.Context, req *iamv1.CreateSessionRequest) (*iamv1.CreateSessionResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	dev, err := s.sessions.GetOrCreateDevice(ctx, userID,
		req.GetDeviceName(), req.GetDeviceType(), req.GetOs(), req.GetBrowser(), req.GetFingerprint(), req.GetDeviceToken())
	if err != nil || dev == nil || dev.ID == "" {
		s.logger.Error("gateway device tracking failed", "user_id", userID, "err", err)
		return nil, status.Error(codes.Internal, "device tracking failed")
	}
	if req.GetTrustForMfa() {
		if err := s.sessions.TrustDevice(ctx, dev.ID, userID, true); err != nil {
			s.logger.Error("gateway device trust failed", "user_id", userID, "device_id", dev.ID, "err", err)
			return nil, status.Error(codes.Internal, "device trust failed")
		}
	}
	sess, err := s.sessions.CreateSession(ctx, userID, dev.ID,
		req.GetHydraSessionId(), req.GetAccessTokenJti(), req.GetRefreshTokenJti(),
		req.GetIpAddress(), req.GetUserAgent())
	if err != nil {
		if errors.Is(err, service.ErrSessionLimitReached) {
			return nil, status.Error(codes.ResourceExhausted, err.Error())
		}
		s.logger.Error("gateway session create failed", "user_id", userID, "err", err)
		return nil, status.Error(codes.Internal, "session create failed")
	}
	s.auditSession(ctx, "session_created", userID, "create", req.GetIpAddress(), req.GetUserAgent(), map[string]any{
		"session_id": sess.ID,
		"device_id":  dev.ID,
	})
	return &iamv1.CreateSessionResponse{
		SessionId: sess.ID,
		DeviceId:  dev.ID,
		ExpiresAt: timestamppb.New(sess.ExpiresAt),
	}, nil
}

func (s *GatewayServer) RevokeSession(ctx context.Context, req *iamv1.RevokeSessionRequest) (*iamv1.RevokeSessionResponse, error) {
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := s.sessions.ForceRevokeSession(ctx, sessionID, "logout"); err != nil {
		s.logger.Error("gateway session revoke failed", "session_id", sessionID, "err", err)
		return nil, status.Error(codes.Internal, "session revoke failed")
	}
	s.auditSession(ctx, "session_revoked", sessionID, "logout", "", "", map[string]any{
		"session_id": sessionID,
		"reason":     "logout",
	})
	return &iamv1.RevokeSessionResponse{}, nil
}

func (s *GatewayServer) TouchSession(ctx context.Context, req *iamv1.TouchSessionRequest) (*iamv1.TouchSessionResponse, error) {
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := s.sessions.TouchSession(ctx, sessionID); err != nil {
		s.logger.Warn("gateway session touch failed", "session_id", sessionID, "err", err)
		return nil, status.Error(codes.Internal, "session touch failed")
	}
	return &iamv1.TouchSessionResponse{}, nil
}

func (s *GatewayServer) GetMfaStatus(ctx context.Context, req *iamv1.GetMfaStatusRequest) (*iamv1.GetMfaStatusResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	settings, err := s.mfa.GetSettings(ctx, userID)
	if err != nil {
		s.logger.Error("gateway mfa status failed", "user_id", userID, "err", err)
		return nil, status.Error(codes.Internal, "mfa status lookup failed")
	}
	resp := &iamv1.GetMfaStatusResponse{}
	if settings != nil {
		resp.IsEnrolled = settings.IsEnrolled
		resp.Method = settings.Method
	}
	return resp, nil
}

func (s *GatewayServer) CheckMfa(ctx context.Context, req *iamv1.CheckMfaRequest) (*iamv1.CheckMfaResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	result, err := s.mfa.CheckMFA(ctx, userID, req.GetDeviceToken())
	if err != nil {
		s.logger.Error("gateway mfa device check failed", "user_id", userID, "err", err)
		return nil, status.Error(codes.Internal, "mfa device check failed")
	}
	return &iamv1.CheckMfaResponse{
		RequiresMfa: result.RequiresMFA,
		CanUseMfa:   result.CanUseMFA,
		Methods:     result.Methods,
	}, nil
}

func (s *GatewayServer) VerifyMfa(ctx context.Context, req *iamv1.VerifyMfaRequest) (*iamv1.VerifyMfaResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if strings.TrimSpace(req.GetCode()) == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}
	if err := s.mfa.VerifyCode(ctx, userID, req.GetCode()); err != nil {
		return nil, s.mapMFAError(err, "mfa verification failed")
	}
	return &iamv1.VerifyMfaResponse{}, nil
}

func (s *GatewayServer) GenerateMfaSecret(ctx context.Context, req *iamv1.GenerateMfaSecretRequest) (*iamv1.GenerateMfaSecretResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	secret, err := s.mfa.GenerateSecret(ctx, userID, req.GetUsername(), req.GetEmail())
	if err != nil || secret == nil {
		s.logger.Error("gateway mfa enrollment secret failed", "user_id", userID, "err", err)
		return nil, status.Error(codes.Internal, "mfa enrollment secret failed")
	}
	return &iamv1.GenerateMfaSecretResponse{Secret: secret.Secret, OtpauthUrl: secret.OTPAuth}, nil
}

func (s *GatewayServer) VerifyMfaEnrollment(ctx context.Context, req *iamv1.VerifyMfaEnrollmentRequest) (*iamv1.VerifyMfaEnrollmentResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if strings.TrimSpace(req.GetCode()) == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}
	backupCodes, err := s.mfa.VerifyAndEnroll(ctx, userID, req.GetCode())
	if err != nil {
		return nil, s.mapMFAError(err, "mfa enrollment verification failed")
	}
	return &iamv1.VerifyMfaEnrollmentResponse{BackupCodes: backupCodes}, nil
}

// mapMFAError: a wrong code is the caller's input (INVALID_ARGUMENT); an
// enrollment or lockout state rule is FAILED_PRECONDITION; anything else is
// an internal failure with a generic message.
func (s *GatewayServer) mapMFAError(err error, internalMessage string) error {
	switch {
	case errors.Is(err, service.ErrMFAInvalidCode), errors.Is(err, service.ErrMFAInvalidEnrollmentCode):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrMFALocked),
		errors.Is(err, service.ErrMFANotEnrolled),
		errors.Is(err, service.ErrMFANoPendingEnrollment),
		errors.Is(err, service.ErrMFAAlreadyEnrolled):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		s.logger.Error(internalMessage, "err", err)
		return status.Error(codes.Internal, internalMessage)
	}
}

// auditSession mirrors the HTTP internal session handlers' audit events. The
// client IP/user agent are the browser's when the request carries them
// (session creation); otherwise the calling workload's peer address is kept so
// the record still says where the call came from.
func (s *GatewayServer) auditSession(ctx context.Context, eventType, subject, action, clientIP, userAgent string, details map[string]any) {
	if s.audit == nil {
		return
	}
	if clientIP == "" {
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			clientIP = p.Addr.String()
		}
	}
	if userAgent == "" {
		userAgent = "grpc:" + gatewaySource
		if claims, ok := interceptors.ServiceClaims(ctx); ok && claims.Source != "" {
			userAgent = "grpc:" + claims.Source
		}
	}
	s.audit.Event(ctx, &domain.AuthEvent{
		EventType: eventType,
		Subject:   subject,
		Action:    action,
		Resource:  "session",
		Result:    "success",
		Details:   details,
		ClientIP:  clientIP,
		UserAgent: userAgent,
		RequestID: ardametadata.FromIncoming(ctx).RequestID,
	})
}

func toProtoUserContext(uc *domain.UserContext) *iamv1.UserContext {
	out := &iamv1.UserContext{
		UserId:                   uc.UserID,
		Subject:                  uc.Subject,
		Username:                 uc.Username,
		Email:                    uc.Email,
		DisplayName:              uc.DisplayName,
		Nickname:                 uc.Nickname,
		FirstName:                uc.FirstName,
		LastName:                 uc.LastName,
		PhoneNumber:              uc.PhoneNumber,
		Birthdate:                uc.Birthdate,
		Gender:                   uc.Gender,
		Address:                  uc.Address,
		Country:                  uc.Country,
		Status:                   uc.Status,
		PictureUrl:               uc.PictureURL,
		AvatarFileId:             uc.AvatarFileID,
		CoverImageUrl:            uc.CoverImageURL,
		CoverFileId:              uc.CoverFileID,
		TenantId:                 uc.TenantID,
		ActiveTenantId:           uc.ActiveTenantID,
		TenantSelectionRequired:  uc.TenantSelectionRequired,
		OrgIds:                   uc.OrgIDs,
		GroupIds:                 uc.GroupIDs,
		Roles:                    uc.Roles,
		Permissions:              uc.Permissions,
		GlobalRoles:              uc.GlobalRoles,
		GlobalPermissions:        uc.GlobalPermissions,
		IsGlobalAdmin:            uc.IsGlobalAdmin,
		GlobalCapabilitiesLoaded: uc.GlobalCapabilitiesLoaded,
		AuthVersion:              uc.AuthVersion,
		Timezone:                 uc.Timezone,
		Locale:                   uc.Locale,
	}
	for _, m := range uc.TenantMemberships {
		out.TenantMemberships = append(out.TenantMemberships, &iamv1.TenantMembership{
			TenantId:     m.TenantID,
			TenantCode:   m.TenantCode,
			TenantName:   m.TenantName,
			TenantStatus: m.TenantStatus,
			Status:       m.Status,
			IsDefault:    m.IsDefault,
		})
	}
	return out
}
