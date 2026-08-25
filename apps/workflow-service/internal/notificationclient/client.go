package notificationclient

import (
	"context"

	grpcnotification "github.com/arda-labs/arda/libs/go/arda-grpc/client/notification"
)

type Client struct{ grpc *grpcnotification.Client }

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
	Type           string
	TitleKey       string
	BodyKey        string
	Href           string
	Params         map[string]any
}

type Recipient struct {
	Type   string `json:"type"`
	UserID string `json:"user_id,omitempty"`
}

func New(addr string) (*Client, error) {
	grpcClient, err := grpcnotification.New(addr, "workflow-service")
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

func (c *Client) Enabled() bool { return c != nil && c.grpc != nil }

func (c *Client) Accept(ctx context.Context, in AcceptRequest) error {
	recipients := make([]grpcnotification.Recipient, 0, len(in.Recipients))
	for _, recipient := range in.Recipients {
		recipients = append(recipients, grpcnotification.Recipient{Type: recipient.Type, UserID: recipient.UserID})
	}
	_, _, err := c.grpc.Accept(ctx, grpcnotification.AcceptRequest{
		TenantID: in.TenantID, IdempotencyKey: in.IdempotencyKey, SourceService: in.SourceService,
		SourceEventID: in.SourceEventID, EventType: in.EventType, TemplateKey: in.TemplateKey,
		Channels: in.Channels, Recipients: recipients, Payload: in.Payload, CorrelationID: in.CorrelationID,
		Type: in.Type, TitleKey: in.TitleKey, BodyKey: in.BodyKey, Href: in.Href, Params: in.Params,
	})
	return err
}
