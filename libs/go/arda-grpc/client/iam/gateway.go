package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/client/retry"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
	"google.golang.org/grpc"
)

// gatewayTimeout bounds one GatewayService call. Session creation touches
// device, trust and session rows; the previous HTTP adapter allowed 10s.
const gatewayTimeout = 10 * time.Second

// GatewayClient calls arda.iam.v1.GatewayService. Only auth-gateway is
// allowed to call it; the server rejects every other source service.
type GatewayClient struct {
	conn    *grpc.ClientConn
	api     iamv1.GatewayServiceClient
	timeout time.Duration
}

// DialGateway connects to IAM at addr with mTLS and a signed service
// identity from sourceService (always "auth-gateway" in production).
func DialGateway(addr, sourceService string) (*GatewayClient, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("iam grpc address is required")
	}
	if strings.TrimSpace(sourceService) == "" {
		return nil, errors.New("iam gateway client requires a source service name")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, fmt.Errorf("iam grpc service identity is not configured: %w", err)
	}
	transportCreds, err := identity.ClientTransportCredentials("iam-service")
	if err != nil {
		return nil, fmt.Errorf("iam grpc tls is not configured: %w", err)
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		// Lookups and the readiness probe are safe to replay. Everything that
		// writes (identity link, session, MFA verification which counts
		// attempts, enrollment) must not be retried by the transport.
		retry.ReadOnly("arda.iam.v1.GatewayService",
			"Ready", "GetUserBySubject", "GetUserById", "GetUserByKratosIdentityId", "GetMfaStatus", "CheckMfa"),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "iam-service"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("dial iam gateway: %w", err)
	}
	conn.Connect()
	return &GatewayClient{conn: conn, api: iamv1.NewGatewayServiceClient(conn), timeout: gatewayTimeout}, nil
}

// NewGatewayClientWithConn builds a client on an existing connection; tests
// use it with an in-memory server.
func NewGatewayClientWithConn(cc grpc.ClientConnInterface) *GatewayClient {
	return &GatewayClient{api: iamv1.NewGatewayServiceClient(cc), timeout: gatewayTimeout}
}

// Close releases the connection.
func (c *GatewayClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *GatewayClient) call(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if c == nil || c.api == nil {
		return nil, nil, errors.New("iam gateway client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	return callCtx, cancel, nil
}

func (c *GatewayClient) Ready(ctx context.Context) error {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = c.api.Ready(ctx, &iamv1.ReadyRequest{})
	return err
}

func (c *GatewayClient) GetUserBySubject(ctx context.Context, subject string) (*iamv1.UserContext, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := c.api.GetUserBySubject(ctx, &iamv1.GetUserBySubjectRequest{Subject: subject})
	if err != nil {
		return nil, err
	}
	return resp.GetUser(), nil
}

// GetUserByID returns the user's default context, or the tenant-scoped one
// when tenantID is set.
func (c *GatewayClient) GetUserByID(ctx context.Context, userID, tenantID string) (*iamv1.UserContext, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := c.api.GetUserById(ctx, &iamv1.GetUserByIdRequest{UserId: userID, TenantId: tenantID})
	if err != nil {
		return nil, err
	}
	return resp.GetUser(), nil
}

func (c *GatewayClient) GetUserByKratosIdentityID(ctx context.Context, identityID string) (*iamv1.UserContext, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := c.api.GetUserByKratosIdentityId(ctx, &iamv1.GetUserByKratosIdentityIdRequest{IdentityId: identityID})
	if err != nil {
		return nil, err
	}
	return resp.GetUser(), nil
}

func (c *GatewayClient) ResolveOrLinkKratosIdentity(ctx context.Context, identityID, email, name string) (*iamv1.UserContext, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := c.api.ResolveOrLinkKratosIdentity(ctx, &iamv1.ResolveOrLinkKratosIdentityRequest{IdentityId: identityID, Email: email, Name: name})
	if err != nil {
		return nil, err
	}
	return resp.GetUser(), nil
}

func (c *GatewayClient) ResolveOrLinkIdentity(ctx context.Context, providerID, externalID, email, name string, emailVerified bool) (*iamv1.UserContext, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := c.api.ResolveOrLinkIdentity(ctx, &iamv1.ResolveOrLinkIdentityRequest{
		ProviderId: providerID, ExternalId: externalID, Email: email, Name: name, EmailVerified: emailVerified,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetUser(), nil
}

func (c *GatewayClient) CreateSession(ctx context.Context, req *iamv1.CreateSessionRequest) (*iamv1.CreateSessionResponse, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return c.api.CreateSession(ctx, req)
}

func (c *GatewayClient) RevokeSession(ctx context.Context, sessionID string) error {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = c.api.RevokeSession(ctx, &iamv1.RevokeSessionRequest{SessionId: sessionID})
	return err
}

func (c *GatewayClient) TouchSession(ctx context.Context, sessionID string) error {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = c.api.TouchSession(ctx, &iamv1.TouchSessionRequest{SessionId: sessionID})
	return err
}

func (c *GatewayClient) GetMFAStatus(ctx context.Context, userID string) (*iamv1.GetMfaStatusResponse, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return c.api.GetMfaStatus(ctx, &iamv1.GetMfaStatusRequest{UserId: userID})
}

func (c *GatewayClient) CheckMFA(ctx context.Context, userID, deviceToken string) (*iamv1.CheckMfaResponse, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return c.api.CheckMfa(ctx, &iamv1.CheckMfaRequest{UserId: userID, DeviceToken: deviceToken})
}

func (c *GatewayClient) VerifyMFA(ctx context.Context, userID, code string) error {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = c.api.VerifyMfa(ctx, &iamv1.VerifyMfaRequest{UserId: userID, Code: code})
	return err
}

func (c *GatewayClient) GenerateMFASecret(ctx context.Context, userID, username, email string) (*iamv1.GenerateMfaSecretResponse, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return c.api.GenerateMfaSecret(ctx, &iamv1.GenerateMfaSecretRequest{UserId: userID, Username: username, Email: email})
}

func (c *GatewayClient) VerifyMFAEnrollment(ctx context.Context, userID, code string) (*iamv1.VerifyMfaEnrollmentResponse, error) {
	ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return c.api.VerifyMfaEnrollment(ctx, &iamv1.VerifyMfaEnrollmentRequest{UserId: userID, Code: code})
}
