package kratos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Identity represents a Kratos identity.
type Identity struct {
	ID       string         `json:"id"`
	SchemaID string         `json:"schema_id"`
	Traits   IdentityTraits `json:"traits"`
	State    string         `json:"state"`
}

// IdentityTraits holds the identity traits.
type IdentityTraits struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// CreateIdentityRequest is the payload to create a Kratos identity.
type CreateIdentityRequest struct {
	SchemaID    string               `json:"schema_id"`
	Traits      IdentityTraits       `json:"traits"`
	Credentials *IdentityCredentials `json:"credentials,omitempty"`
	State       string               `json:"state,omitempty"`
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

// FindIdentityByIdentifier retrieves an identity by a credential identifier such as email.
func (c *Client) FindIdentityByIdentifier(identifier string) (*Identity, error) {
	endpoint := fmt.Sprintf("%s/admin/identities?credentials_identifier=%s", c.adminURL, url.QueryEscape(identifier))
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("kratos list identities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kratos list identities: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var identities []Identity
	if err := json.NewDecoder(resp.Body).Decode(&identities); err != nil {
		return nil, fmt.Errorf("kratos list identities decode: %w", err)
	}
	if len(identities) == 0 {
		return nil, nil
	}
	return &identities[0], nil
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

// UpdateIdentityEmail updates the traits of an identity in Kratos.
func (c *Client) UpdateIdentityEmail(id, email, name string) error {
	req := struct {
		SchemaID string         `json:"schema_id"`
		Traits   IdentityTraits `json:"traits"`
		State    string         `json:"state"`
	}{
		SchemaID: "default",
		Traits: IdentityTraits{
			Email: email,
			Name:  name,
		},
		State: "active",
	}

	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/admin/identities/%s", c.adminURL, id)

	request, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("kratos update request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("kratos update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kratos update: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// UpdateIdentityPassword replaces the password credential for an identity.
func (c *Client) UpdateIdentityPassword(id, password string) error {
	identity, err := c.GetIdentity(id)
	if err != nil {
		return err
	}
	if identity == nil {
		return fmt.Errorf("identity not found")
	}

	req := struct {
		SchemaID    string               `json:"schema_id"`
		Traits      IdentityTraits       `json:"traits"`
		Credentials *IdentityCredentials `json:"credentials,omitempty"`
		State       string               `json:"state"`
	}{
		SchemaID: identity.SchemaID,
		Traits:   identity.Traits,
		Credentials: &IdentityCredentials{
			Password: &PasswordConfig{
				Config: PasswordConfigInner{Password: password},
			},
		},
		State: identity.State,
	}
	if req.SchemaID == "" {
		req.SchemaID = "default"
	}
	if req.State == "" {
		req.State = "active"
	}

	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/admin/identities/%s", c.adminURL, id)
	request, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("kratos password update request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("kratos password update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kratos password update: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
