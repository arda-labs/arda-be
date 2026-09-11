// Package hydra is a thin HTTP client for the Ory Hydra admin API (X3).
// Only the OAuth2 client management subset used by the admin UI is covered.
package hydra

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

const defaultTimeout = 10 * time.Second

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" }

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hydra admin request: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("hydra admin %d: %s", resp.StatusCode, truncate(string(payload), 300))
	}
	return payload, nil
}

// ListClients returns OAuth2 clients (accepts array or {items:[]} shapes).
func (c *Client) ListClients(ctx context.Context) ([]map[string]any, error) {
	payload, err := c.do(ctx, http.MethodGet, "/admin/clients", nil)
	if err != nil {
		return nil, err
	}
	var list []map[string]any
	if err := json.Unmarshal(payload, &list); err == nil {
		return list, nil
	}
	var wrapper struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return nil, fmt.Errorf("decode hydra clients: %w", err)
	}
	return wrapper.Items, nil
}

// GetClient returns one client by id.
func (c *Client) GetClient(ctx context.Context, id string) (map[string]any, error) {
	payload, err := c.do(ctx, http.MethodGet, "/admin/clients/"+id, nil)
	if err != nil {
		return nil, err
	}
	var client map[string]any
	if err := json.Unmarshal(payload, &client); err != nil {
		return nil, fmt.Errorf("decode hydra client: %w", err)
	}
	return client, nil
}

// CreateClient registers one OAuth2 client.
func (c *Client) CreateClient(ctx context.Context, client map[string]any) (map[string]any, error) {
	payload, err := c.do(ctx, http.MethodPost, "/admin/clients", client)
	if err != nil {
		return nil, err
	}
	var created map[string]any
	if err := json.Unmarshal(payload, &created); err != nil {
		return nil, fmt.Errorf("decode hydra client: %w", err)
	}
	return created, nil
}

// UpdateClient replaces one OAuth2 client.
func (c *Client) UpdateClient(ctx context.Context, id string, client map[string]any) (map[string]any, error) {
	payload, err := c.do(ctx, http.MethodPut, "/admin/clients/"+id, client)
	if err != nil {
		return nil, err
	}
	var updated map[string]any
	if err := json.Unmarshal(payload, &updated); err != nil {
		return nil, fmt.Errorf("decode hydra client: %w", err)
	}
	return updated, nil
}

// DeleteClient removes one OAuth2 client.
func (c *Client) DeleteClient(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/admin/clients/"+id, nil)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
