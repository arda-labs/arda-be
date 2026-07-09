package notificationclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type AcceptRequest struct {
	TenantID       string         `json:"tenant_id"`
	IdempotencyKey string         `json:"idempotency_key"`
	SourceService  string         `json:"source_service"`
	SourceEventID  string         `json:"source_event_id"`
	EventType      string         `json:"event_type"`
	TemplateKey    string         `json:"template_key"`
	Channels       []string       `json:"channels"`
	Recipients     []Recipient    `json:"recipients"`
	Payload        map[string]any `json:"payload,omitempty"`
	CorrelationID  string         `json:"correlation_id,omitempty"`
	Type           string         `json:"type,omitempty"`
	TitleKey       string         `json:"title_key,omitempty"`
	BodyKey        string         `json:"body_key,omitempty"`
	Href           string         `json:"href,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
}

type Recipient struct {
	Type   string `json:"type"`
	UserID string `json:"user_id,omitempty"`
}

func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) Accept(ctx context.Context, in AcceptRequest) error {
	if !c.Enabled() {
		return nil
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/notifications", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("notification accept status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
}
