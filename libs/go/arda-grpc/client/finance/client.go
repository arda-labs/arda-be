// Package finance is the gRPC client for finance-service PostingService.
// Callers: workflow-service workers (posting steps) and any internal flow
// that posts journal entries. Tenant scope travels through gRPC metadata —
// set ardametadata.Context on the outgoing context before calling.
package finance

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 10 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     financev1.PostingServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("finance grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("finance grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("finance-service")
	if err != nil {
		return nil, errors.New("finance grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "finance-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:    conn,
		api:     financev1.NewPostingServiceClient(conn),
		timeout: defaultTimeout,
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

// Validate resolves the posting without writing (FE preview path uses HTTP;
// this is for workers doing dry-run checks).
func (c *Client) Validate(ctx context.Context, req *financev1.PostingRequest) (*financev1.ValidationResult, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.ValidatePosting(callCtx, req)
}

// Post writes one balanced journal entry. Idempotent per idempotency_key.
func (c *Client) Post(ctx context.Context, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.PostTransaction(callCtx, req)
}

// Reverse creates the reversal entry for a posted journal entry.
func (c *Client) Reverse(ctx context.Context, req *financev1.ReverseRequest) (*financev1.PostingResponse, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.ReverseTransaction(callCtx, req)
}
