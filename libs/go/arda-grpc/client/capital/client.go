// Package capital is the gRPC client for capital-service
// CapitalCommandService (CFC lifecycle callback surface).
package capital

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	capitalv1 "github.com/arda-labs/arda/libs/go/arda-proto/capital/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

// Requester is the narrow surface the workflow CFC workers need.
type Requester interface {
	CheckRequest(ctx context.Context, kind, refID string) (bool, string, error)
	ResolveRequest(ctx context.Context, kind, refID, decision, actor string) error
}

type Client struct {
	conn    *grpc.ClientConn
	api     capitalv1.CapitalCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("capital grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("capital grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("capital-service")
	if err != nil {
		return nil, errors.New("capital grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "capital-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, api: capitalv1.NewCapitalCommandServiceClient(conn), timeout: defaultTimeout}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) CheckRequest(ctx context.Context, kind, refID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("capital client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckRequest(callCtx, &capitalv1.CheckRequestRequest{Kind: kind, RefId: refID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) ResolveRequest(ctx context.Context, kind, refID, decision, actor string) error {
	if c == nil {
		return errors.New("capital client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveRequest(callCtx, &capitalv1.ResolveRequestRequest{
		Kind:     kind,
		RefId:    refID,
		Decision: decision,
		Actor:    actor,
	})
	return err
}
