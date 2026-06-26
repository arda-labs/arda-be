package iamclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// UserContext mirrors the IAM internal API response.
type UserContext struct {
	UserID      string   `json:"userId"`
	Subject     string   `json:"subject"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	TenantID    string   `json:"tenantId"`
	OrgIDs      []string `json:"orgIds"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

// Client calls the IAM service internal APIs.
type Client struct {
	baseURL string
	client  *http.Client
}

// New creates a new IAM client.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

// GetUserBySubject fetches a user context by external subject.
func (c *Client) GetUserBySubject(ctx context.Context, subject string) (*UserContext, error) {
	url := c.baseURL + "/internal/iam/users/by-subject/" + subject
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iam returned status %d", resp.StatusCode)
	}

	var uc UserContext
	if err := json.NewDecoder(resp.Body).Decode(&uc); err != nil {
		return nil, fmt.Errorf("decode iam response: %w", err)
	}
	return &uc, nil
}
