package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultTimeout = 5 * time.Second

type UserInfo struct {
	ID        string
	Name      string
	Email     string
	AvatarURL string
}

type Client struct {
	conn    *grpc.ClientConn
	api     iamv1.UserServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("iam grpc address is required")
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{})),
	)
	if err != nil {
		return nil, fmt.Errorf("dial iam: %w", err)
	}
	client := &Client{
		conn:    conn,
		api:     iamv1.NewUserServiceClient(conn),
		timeout: defaultTimeout,
	}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) GetUserBatch(ctx context.Context, userIDs []string) (map[string]UserInfo, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.api.GetUserBatch(ctx, &iamv1.GetUserBatchRequest{UserIds: userIDs})
	if err != nil {
		return nil, fmt.Errorf("get user batch: %w", err)
	}

	result := make(map[string]UserInfo, len(resp.Users))
	for _, u := range resp.Users {
		result[u.Id] = UserInfo{
			ID:        u.Id,
			Name:      u.Name,
			Email:     u.Email,
			AvatarURL: u.AvatarUrl,
		}
	}
	return result, nil
}
