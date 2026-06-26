package kratos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Identity represents a Kratos identity.
type Identity struct {
	ID       string                `json:"id"`
	SchemaID string                `json:"schema_id"`
	Traits   IdentityTraits        `json:"traits"`
	State    string                `json:"state"`
}

// IdentityTraits holds the identity traits.
type IdentityTraits struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// CreateIdentityRequest is the payload to create a Kratos identity.
type CreateIdentityRequest struct {
	SchemaID    string              `json:"schema_id"`
	Traits      IdentityTraits      `json:"traits"`
	Credentials *IdentityCredentials `json:"credentials,omitempty"`
	State       string              `json:"state,omitempty"`
}

// IdentityCredentials holds password credentials.
type IdentityCredentials struct {
	Password *PasswordConfig `json:"password"`
}

type PasswordConfig struct {
	Config PasswordConfigInner `json:"config"`
}

type PasswordConfigInner struct {
	Password string `json:"password"`
}

// Client communicates with Kratos Admin API.
type Client struct {
	adminURL   string
	httpClient *http.Client
}

// New creates a Kratos Admin client.
func New(adminURL string) *Client {
	return &Client{
		adminURL: adminURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// CreateIdentity creates a new identity in Kratos.
func (c *Client) CreateIdentity(email, password, name string) (*Identity, error) {
	req := CreateIdentityRequest{
		SchemaID: "default",
		Traits: IdentityTraits{
			Email: email,
			Name:  name,
		},
		State: "active",
	}
	if password != "" {
		req.Credentials = &IdentityCredentials{
			Password: &PasswordConfig{
				Config: PasswordConfigInner{
					Password: password,
				},
			},
		}
	}

	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/admin/identities", c.adminURL)

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("kratos create: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("identity already exists")
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("kratos create: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var identity Identity
	if err := json.Unmarshal(respBody, &identity); err != nil {
		return nil, fmt.Errorf("kratos decode: %w", err)
	}
	return &identity, nil
}

// GetIdentity retrieves an identity by ID.
func (c *Client) GetIdentity(id string) (*Identity, error) {
	url := fmt.Sprintf("%s/admin/identities/%s", c.adminURL, id)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("kratos get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kratos get: HTTP %d", resp.StatusCode)
	}

	var identity Identity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		return nil, fmt.Errorf("kratos decode: %w", err)
	}
	return &identity, nil
}

// DeleteIdentity removes an identity by ID.
func (c *Client) DeleteIdentity(id string) error {
	url := fmt.Sprintf("%s/admin/identities/%s", c.adminURL, id)
	req, _ := http.NewRequest("DELETE", url, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kratos delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kratos delete: HTTP %d", resp.StatusCode)
	}
	return nil
}
