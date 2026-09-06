// Package hrm is the gRPC client for hrm-service EmployeeCommandService.
package hrm

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	hrmv1 "github.com/arda-labs/arda/libs/go/arda-proto/hrm/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     hrmv1.EmployeeCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("hrm grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("hrm grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("hrm-service")
	if err != nil {
		return nil, errors.New("hrm grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "hrm-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, api: hrmv1.NewEmployeeCommandServiceClient(conn), timeout: defaultTimeout}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) CheckRegistration(ctx context.Context, registrationID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("hrm client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckRegistration(callCtx, &hrmv1.CheckRegistrationRequest{RegistrationId: registrationID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) SettleRegistration(ctx context.Context, registrationID, approvedBy string) (string, error) {
	if c == nil {
		return "", errors.New("hrm client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.SettleRegistration(callCtx, &hrmv1.SettleRegistrationRequest{
		RegistrationId: registrationID,
		ApprovedBy:     approvedBy,
	})
	if err != nil {
		return "", err
	}
	return resp.GetEmployeeId(), nil
}

func (c *Client) RejectRegistration(ctx context.Context, registrationID, decidedBy, note string) error {
	if c == nil {
		return errors.New("hrm client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.RejectRegistration(callCtx, &hrmv1.RejectRegistrationRequest{
		RegistrationId: registrationID,
		DecidedBy:      decidedBy,
		Note:           note,
	})
	return err
}
