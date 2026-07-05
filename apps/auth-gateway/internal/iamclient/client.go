package iamclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// UserContext mirrors the IAM internal API response.
type UserContext struct {
	UserID        string   `json:"userId"`
	Subject       string   `json:"subject"`
	Username      string   `json:"username"`
	Email         string   `json:"email"`
	DisplayName   string   `json:"displayName,omitempty"`
	Nickname      string   `json:"nickname,omitempty"`
	FirstName     string   `json:"firstName,omitempty"`
	LastName      string   `json:"lastName,omitempty"`
	PhoneNumber   string   `json:"phoneNumber,omitempty"`
	Birthdate     string   `json:"birthdate,omitempty"`
	Gender        string   `json:"gender,omitempty"`
	Address       string   `json:"address,omitempty"`
	Country       string   `json:"country,omitempty"`
	PictureURL    string   `json:"picture,omitempty"`
	AvatarFileID  string   `json:"avatarFileId,omitempty"`
	CoverImageURL string   `json:"coverImage,omitempty"`
	CoverFileID   string   `json:"coverFileId,omitempty"`
	TenantID      string   `json:"tenantId"`
	OrgIDs        []string `json:"orgIds"`
	GroupIDs      []string `json:"groupIds"`
	Roles         []string `json:"roles"`
	Permissions   []string `json:"permissions"`
	AuthVersion   int64    `json:"authVersion"`
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
	DeviceToken     string `json:"deviceToken"`
	TrustForMFA     bool   `json:"trustForMfa"`
}

// CreateSessionResponse is the response from IAM internal session creation.
type CreateSessionResponse struct {
	SessionID string    `json:"sessionId"`
	DeviceID  string    `json:"deviceId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type MFAStatus struct {
	IsEnrolled bool   `json:"is_enrolled"`
	Method     string `json:"method"`
}

type MFASecret struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

type MFAEnrollResponse struct {
	Status      string   `json:"status"`
	BackupCodes []string `json:"backup_codes"`
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

func (c *Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health/ready", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("iam ready request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iam ready returned status %d", resp.StatusCode)
	}
	return nil
}

// GetUserBySubject fetches a user context by external subject.
func (c *Client) GetUserBySubject(ctx context.Context, subject string) (*UserContext, error) {
	endpoint := c.baseURL + "/internal/iam/users/by-subject/" + url.PathEscape(subject)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	return c.doUserContext(req)
}

// GetUserByID fetches a user context by internal IAM UUID.
func (c *Client) GetUserByID(ctx context.Context, id string) (*UserContext, error) {
	endpoint := c.baseURL + "/internal/iam/users/by-id/" + url.PathEscape(id) + "/context"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	return c.doUserContext(req)
}

func (c *Client) GetUserByKratosIdentityID(ctx context.Context, identityID string) (*UserContext, error) {
	endpoint := c.baseURL + "/internal/iam/users/by-kratos-identity/" + url.PathEscape(identityID) + "/context"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	return c.doUserContext(req)
}

func (c *Client) ResolveOrLinkKratosIdentity(ctx context.Context, identityID, email, name string) (*UserContext, error) {
	body, err := json.Marshal(map[string]string{
		"identityId": identityID,
		"email":      email,
		"name":       name,
	})
	if err != nil {
		return nil, err
	}
	endpoint := c.baseURL + "/internal/iam/users/resolve-kratos-identity"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doUserContext(req)
}

func (c *Client) ResolveOrLinkIdentity(ctx context.Context, providerID, externalID, email, name string, emailVerified bool) (*UserContext, error) {
	body, err := json.Marshal(map[string]any{
		"providerId":    providerID,
		"externalId":    externalID,
		"email":         email,
		"name":          name,
		"emailVerified": emailVerified,
	})
	if err != nil {
		return nil, err
	}
	endpoint := c.baseURL + "/internal/iam/users/resolve-identity"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doUserContext(req)
}

func (c *Client) doUserContext(req *http.Request) (*UserContext, error) {
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

func (c *Client) GetMFAStatus(ctx context.Context, userID string) (*MFAStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/iam/me/mfa/status", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-User-Id", userID)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mfa status request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mfa status returned status %d", resp.StatusCode)
	}
	var status MFAStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode mfa status: %w", err)
	}
	return &status, nil
}

func (c *Client) VerifyMFA(ctx context.Context, userID, code string) error {
	body, err := json.Marshal(map[string]string{"userId": userID, "code": code})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/iam/me/mfa/verify", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("mfa verify request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mfa verify returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GenerateMFASecret(ctx context.Context, userID, username, email string) (*MFASecret, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/iam/me/mfa/enroll", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-User-Id", userID)
	req.Header.Set("X-Username", username)
	req.Header.Set("X-User-Email", email)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mfa enroll request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mfa enroll returned status %d", resp.StatusCode)
	}
	var secret MFASecret
	if err := json.NewDecoder(resp.Body).Decode(&secret); err != nil {
		return nil, fmt.Errorf("decode mfa secret: %w", err)
	}
	return &secret, nil
}

func (c *Client) VerifyMFAEnrollment(ctx context.Context, userID, code string) (*MFAEnrollResponse, error) {
	body, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/iam/me/mfa/verify-enroll", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", userID)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mfa verify enroll request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mfa verify enroll returned status %d", resp.StatusCode)
	}
	var result MFAEnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode mfa enroll response: %w", err)
	}
	return &result, nil
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
