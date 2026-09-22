package crm

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/client/retry"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	crmv1 "github.com/arda-labs/arda/libs/go/arda-proto/crm/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     crmv1.CustomerCommandServiceClient
	member  crmv1.MemberCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("crm grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("crm grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("crm-service")
	if err != nil {
		return nil, errors.New("crm grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		// Only read-only RPCs are retried; command writes are never replayed.
		retry.ReadOnly("arda.crm.v1.CustomerCommandService", "CheckDuplicateIdentity"),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "crm-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:    conn,
		api:     crmv1.NewCustomerCommandServiceClient(conn),
		member:  crmv1.NewMemberCommandServiceClient(conn),
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

func (c *Client) UpdateCustomerStatus(ctx context.Context, customerID, status string) error {
	if c == nil {
		return errors.New("crm client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.UpdateCustomerStatus(callCtx, &crmv1.UpdateCustomerStatusRequest{
		CustomerId: customerID,
		Status:     status,
	})
	return err
}

func (c *Client) CheckDuplicateIdentity(ctx context.Context, customerID string) (bool, error) {
	if c == nil {
		return false, errors.New("crm client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckDuplicateIdentity(callCtx, &crmv1.CheckDuplicateIdentityRequest{
		CustomerId: customerID,
	})
	if err != nil {
		return false, err
	}
	return resp.GetDuplicateFound(), nil
}

// CheckMemberRequest asks crm-service whether a staged capital request may be
// approved (member active, withdrawal within the stake). Read-only, so it is
// goroutine-safe to retry.
func (c *Client) CheckMemberRequest(ctx context.Context, requestID string) (bool, string, error) {
	if c == nil || c.member == nil {
		return false, "", errors.New("crm member client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.member.CheckMemberRequest(callCtx, &crmv1.CheckMemberRequestRequest{RequestId: requestID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

// ResolveMemberRequest records the checker decision; APPROVE moves the stake.
func (c *Client) ResolveMemberRequest(ctx context.Context, requestID, decision, actor string, dataVersion int64) error {
	if c == nil || c.member == nil {
		return errors.New("crm member client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.member.ResolveMemberRequest(callCtx, &crmv1.ResolveMemberRequestRequest{
		RequestId:   requestID,
		Decision:    decision,
		Actor:       actor,
		DataVersion: dataVersion,
	})
	return err
}
