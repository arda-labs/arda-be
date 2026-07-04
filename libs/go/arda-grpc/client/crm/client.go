package crm

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	crmv1 "github.com/arda-labs/arda/libs/go/arda-proto/crm/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultTimeout = 5 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     crmv1.CustomerCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("crm grpc address is required")
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
		api:     crmv1.NewCustomerCommandServiceClient(conn),
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
