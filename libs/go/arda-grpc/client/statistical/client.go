// Package statistical is the gRPC client for statistical-service
// StatisticalCommandService (rpt-submit-v2 callback surface).
package statistical

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	statisticalv1 "github.com/arda-labs/arda/libs/go/arda-proto/statistical/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

// SubmissionResolver is the narrow surface the workflow rpt-submit workers need.
type SubmissionResolver interface {
	CheckSubmission(ctx context.Context, submissionID string) (bool, string, error)
	ResolveSubmission(ctx context.Context, submissionID, decision, actor, note string) error
}

type Client struct {
	conn    *grpc.ClientConn
	api     statisticalv1.StatisticalCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("statistical grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("statistical grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("statistical-service")
	if err != nil {
		return nil, errors.New("statistical grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "statistical-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, api: statisticalv1.NewStatisticalCommandServiceClient(conn), timeout: defaultTimeout}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) CheckSubmission(ctx context.Context, submissionID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("statistical client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckSubmission(callCtx, &statisticalv1.CheckSubmissionRequest{SubmissionId: submissionID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) ResolveSubmission(ctx context.Context, submissionID, decision, actor, note string) error {
	if c == nil {
		return errors.New("statistical client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveSubmission(callCtx, &statisticalv1.ResolveSubmissionRequest{
		SubmissionId: submissionID,
		Decision:     decision,
		Actor:        actor,
		Note:         note,
	})
	return err
}
