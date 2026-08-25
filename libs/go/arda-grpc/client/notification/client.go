package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	notificationv1 "github.com/arda-labs/arda/libs/go/arda-proto/notification/v1"
	"google.golang.org/grpc"
)

type Recipient struct {
	Type    string `json:"type"`
	Address string `json:"address,omitempty"`
	UserID  string `json:"user_id,omitempty"`
}

type AcceptRequest struct {
	TenantID       string
	IdempotencyKey string
	SourceService  string
	SourceEventID  string
	EventType      string
	TemplateKey    string
	Channels       []string
	Recipients     []Recipient
	Payload        map[string]any
	CorrelationID  string
	Priority       int
	Type           string
	TitleKey       string
	BodyKey        string
	Href           string
	Params         map[string]any
}

type Client struct {
	conn    *grpc.ClientConn
	api     notificationv1.NotificationServiceClient
	timeout time.Duration
}

func New(addr, sourceService string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	sourceService = strings.TrimSpace(sourceService)
	if addr == "" || sourceService == "" {
		return nil, fmt.Errorf("notification grpc address and source service are required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, err
	}
	creds, err := identity.ClientTransportCredentials("notification-service")
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "notification-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return &Client{conn: conn, api: notificationv1.NewNotificationServiceClient(conn), timeout: 5 * time.Second}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Accept(ctx context.Context, in AcceptRequest) (string, string, error) {
	if c == nil || c.conn == nil || c.api == nil {
		return "", "", fmt.Errorf("notification grpc client is not configured")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	recipients, err := json.Marshal(in.Recipients)
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(in.Payload)
	if err != nil {
		return "", "", err
	}
	params, err := json.Marshal(in.Params)
	if err != nil {
		return "", "", err
	}
	res, err := c.api.AcceptNotification(callCtx, &notificationv1.AcceptNotificationRequest{
		TenantId: in.TenantID, IdempotencyKey: in.IdempotencyKey, SourceService: in.SourceService,
		SourceEventId: in.SourceEventID, EventType: in.EventType, TemplateKey: in.TemplateKey,
		Channels: in.Channels, RecipientsJson: string(recipients), PayloadJson: string(payload),
		CorrelationId: in.CorrelationID, Priority: int32(in.Priority), Type: in.Type, TitleKey: in.TitleKey,
		BodyKey: in.BodyKey, Href: in.Href, ParamsJson: string(params),
	})
	if err != nil {
		return "", "", err
	}
	return res.GetNotificationId(), res.GetStatus(), nil
}
