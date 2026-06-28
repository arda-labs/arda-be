package ardamedia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type Client struct {
	endpoint   string
	httpClient *http.Client
}

func NewClient() *Client {
	endpoint := os.Getenv("MEDIA_SERVICE_URL")
	if endpoint == "" {
		endpoint = "http://localhost:8092"
	}
	return &Client{
		endpoint:   strings.TrimSuffix(endpoint, "/"),
		httpClient: &http.Client{},
	}
}

func (c *Client) Attach(ctx context.Context, publicIDs []string, ownerType, ownerID string, originalReq *http.Request) error {
	if len(publicIDs) == 0 {
		return nil
	}

	url := c.endpoint + "/api/media/attach"
	payload := map[string]any{
		"public_ids": publicIDs,
		"owner_type": ownerType,
		"owner_id":   ownerID,
	}
	bodyData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Forward tracing/auth headers if original request is present
	if originalReq != nil {
		req.Header.Set("X-Tenant-Id", originalReq.Header.Get("X-Tenant-Id"))
		req.Header.Set("X-Org-Id", originalReq.Header.Get("X-Org-Id"))
		req.Header.Set("X-User-Id", originalReq.Header.Get("X-User-Id"))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to attach files: status %d", resp.StatusCode)
	}
	return nil
}
