package iamclient

import (
	"bytes"
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

// CreateSessionRequest is sent to IAM internal API to create a session record.
type CreateSessionRequest struct {
	UserID          string `json:"userId"`
	HydraSessionID  string `json:"hydraSessionId"`
	AccessTokenJTI  string `json:"jti"`
	RefreshTokenJTI string `json:"refreshJti"`
	IPAddress       string `json:"ip"`
	UserAgent       string `json:"userAgent"`
	DeviceName      string `json:"deviceName"`
	DeviceType      string `json:"deviceType"`
	OS              string `json:"os"`
	Browser         string `json:"browser"`
	Fingerprint     string `json:"fingerprint"`
}

// CreateSessionResponse is the response from IAM internal session creation.
type CreateSessionResponse struct {
	SessionID string    `json:"sessionId"`
	DeviceID  string    `json:"deviceId"`
	ExpiresAt time.Time `json:"expiresAt"`
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
		client:  &http.Client{Timeout: 10 * time.Second},
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

// CreateSession calls the IAM internal API to create a session tracking record.
func (c *Client) CreateSession(ctx context.Context, req *CreateSessionRequest) (*CreateSessionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := c.baseURL + "/internal/iam/sessions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("create session request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("session limit reached: %s", errResp.Error)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create session returned status %d", resp.StatusCode)
	}

	var result CreateSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &result, nil
}

// RevokeSession calls IAM internal API to revoke a session.
func (c *Client) RevokeSession(ctx context.Context, sessionID string) error {
	url := c.baseURL + "/internal/iam/sessions/" + sessionID
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("revoke session request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke session returned status %d", resp.StatusCode)
	}
	return nil
}

// InternalBaseURL returns the base URL for IAM internal API calls.
func (c *Client) InternalBaseURL() string {
	return c.baseURL
}

// DeviceFingerprint builds a device fingerprint for tracking.
type DeviceFingerprint struct {
	UserAgent string
	IP        string
}

// Hash returns a simple device fingerprint string.
func (f DeviceFingerprint) Hash() string {
	// Simple fingerprint — truncate user agent + mask IP
	ua := f.UserAgent
	if len(ua) > 128 {
		ua = ua[:128]
	}
	ip := maskIP(f.IP)
	return fmt.Sprintf("%s|%s", ua, ip)
}

func maskIP(ip string) string {
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == '.' || ip[i] == ':' {
			return ip[:i] + ".0"
		}
	}
	return ip
}
