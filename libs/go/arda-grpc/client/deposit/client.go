// Package deposit is the gRPC client for deposit-service DepositCommandService.
package deposit

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	depositv1 "github.com/arda-labs/arda/libs/go/arda-proto/deposit/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

// Settler is the narrow surface the workflow deposit workers need.
type Settler interface {
	CheckSettle(ctx context.Context, savingsCode string) (bool, string, error)
	Settle(ctx context.Context, savingsCode, actor string) error
}

type Client struct {
	conn    *grpc.ClientConn
	api     depositv1.DepositCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("deposit grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("deposit grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("deposit-service")
	if err != nil {
		return nil, errors.New("deposit grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "deposit-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, api: depositv1.NewDepositCommandServiceClient(conn), timeout: defaultTimeout}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) CheckSettle(ctx context.Context, savingsCode string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckSettle(callCtx, &depositv1.CheckSettleRequest{SavingsCode: savingsCode})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) Settle(ctx context.Context, savingsCode, actor string) error {
	if c == nil {
		return errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.Settle(callCtx, &depositv1.SettleRequest{SavingsCode: savingsCode, Actor: actor})
	return err
}
