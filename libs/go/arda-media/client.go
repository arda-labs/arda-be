package ardamedia

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	mediaclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/media"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

type Client struct {
	grpc *mediaclient.Client
}

func NewClient(sourceService string) (*Client, error) {
	addr := strings.TrimSpace(os.Getenv("MEDIA_GRPC_ADDR"))
	grpcClient, err := mediaclient.Dial(context.Background(), addr, sourceService, nil)
	if err != nil {
		return nil, err
	}
	return &Client{grpc: grpcClient}, nil
}

func (c *Client) Close() error {
	if c == nil || c.grpc == nil {
		return nil
	}
	return c.grpc.Close()
}

func (c *Client) Attach(ctx context.Context, publicIDs []string, ownerType, ownerID string, originalReq *http.Request) error {
	if len(publicIDs) == 0 {
		return nil
	}
	if c == nil || c.grpc == nil {
		return fmt.Errorf("media grpc client is not configured")
	}
	// Convert only the already-verified inbound context into outgoing gRPC
	// metadata. The media service never trusts browser-supplied identity headers
	// or an unauthenticated HTTP call.
	if originalReq != nil {
		ctx = ardametadata.AppendToOutgoing(ctx, ardametadata.FromHTTPHeaders(originalReq.Header))
	}
	return c.grpc.Attach(ctx, publicIDs, ownerType, ownerID)
}
