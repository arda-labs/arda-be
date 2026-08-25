package media

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	mediav1 "github.com/arda-labs/arda/libs/go/arda-proto/media/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 2 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     mediav1.MediaServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	addr = strings.TrimSpace(addr)
	sourceService = strings.TrimSpace(sourceService)
	if addr == "" {
		return nil, errors.New("media grpc address is required")
	}
	if sourceService == "" {
		return nil, errors.New("media grpc source service is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("media grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("media-service")
	if err != nil {
		return nil, errors.New("media grpc tls is not configured: " + err.Error())
	}
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "media-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return &Client{conn: conn, api: mediav1.NewMediaServiceClient(conn), timeout: defaultTimeout}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Attach(ctx context.Context, publicIDs []string, ownerType, ownerID string) error {
	if c == nil || c.api == nil {
		return errors.New("media grpc client is not configured")
	}
	if len(publicIDs) == 0 {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.AttachFiles(callCtx, &mediav1.AttachFilesRequest{
		PublicIds:  publicIDs,
		OwnerType: strings.TrimSpace(ownerType),
		OwnerId:   strings.TrimSpace(ownerID),
	})
	return err
}
