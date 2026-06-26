package introspection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result is the response from an OAuth2 token introspection endpoint.
type Result struct {
	Active    bool     `json:"active"`
	Subject   string   `json:"sub"`
	Scope     string   `json:"scope"`
	ClientID  string   `json:"client_id"`
	TokenType string   `json:"token_type"`
	ExpiresAt int64    `json:"exp"`
	Issuer    string   `json:"iss"`
	Audience  []string `json:"aud"`
}

// Client calls an OAuth2 introspection endpoint.
type Client struct {
	endpoint     string
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

// New creates a new introspection client.
func New(endpoint, clientID, clientSecret string) *Client {
	return &Client{
		endpoint:     endpoint,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}
}

// Introspect validates a token with the OAuth2 introspection endpoint.
func (c *Client) Introspect(ctx context.Context, token string) (*Result, error) {
	if token == "" {
		return nil, fmt.Errorf("token is empty")
	}

	data := url.Values{}
	data.Set("token", token)
	data.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.clientID, c.clientSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("introspection request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("introspection returned status %d", resp.StatusCode)
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode introspection response: %w", err)
	}

	if !result.Active {
		return nil, fmt.Errorf("token is not active")
	}

	return &result, nil
}
