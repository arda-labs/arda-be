package platform

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultTimeout = 2 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     platformv1.PlatformServiceClient
	timeout time.Duration
	logger  *slog.Logger
}

type Scope struct {
	TenantID  string
	ScopeType string
	ScopeID   string
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("platform grpc address is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{})),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:    conn,
		api:     platformv1.NewPlatformServiceClient(conn),
		timeout: defaultTimeout,
		logger:  logger,
	}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) ResolveString(ctx context.Context, tenantID, key string) (string, error) {
	return c.ResolveStringWithScopes(ctx, tenantID, key)
}

func (c *Client) ResolveStringWithScopes(ctx context.Context, tenantID, key string, scopes ...Scope) (string, error) {
	if c == nil {
		return "", errors.New("platform client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req := &platformv1.ResolveParameterRequest{
		TenantId: tenantID,
		Key:      key,
		Scopes:   make([]*platformv1.ScopeSelector, 0, len(scopes)),
	}
	for _, scope := range scopes {
		req.Scopes = append(req.Scopes, &platformv1.ScopeSelector{
			TenantId:  scope.TenantID,
			ScopeType: scope.ScopeType,
			ScopeId:   scope.ScopeID,
		})
	}
	resp, err := c.api.ResolveParameter(callCtx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.GetValue()), nil
}
