package saml

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/provider"
)

// Config holds SAML identity provider configuration.
type Config struct {
	ProviderID    string   `yaml:"id"`
	ProviderName  string   `yaml:"name"`
	MetadataURL   string   `yaml:"metadata_url"`
	ACSURL        string   `yaml:"acs_url"`
	EntityID      string   `yaml:"entity_id"`
	SigningCert   string   `yaml:"signing_cert"`
	PrivateKey    string   `yaml:"private_key"`
	Domains       []string `yaml:"domains"`
	Priority      int      `yaml:"priority"`
}

// SAMLProvider authenticates users via SAML 2.0 (ADFS, PingFederate).
type SAMLProvider struct {
	config Config
}

// New creates a SAML provider. Ready for implementation.
func New(cfg Config) *SAMLProvider {
	return &SAMLProvider{config: cfg}
}

func (p *SAMLProvider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          p.config.ProviderID,
		Type:        provider.TypeSAML,
		Name:        p.config.ProviderName,
		Description: fmt.Sprintf("Đăng nhập bằng %s", p.config.ProviderName),
		Priority:    p.config.Priority,
		IsEnabled:   true,
		Domains:     p.config.Domains,
	}
}

func (p *SAMLProvider) Validate(ctx context.Context) error {
	if p.config.MetadataURL == "" && p.config.EntityID == "" {
		return fmt.Errorf("saml %q: metadata_url or entity_id required", p.config.ProviderID)
	}
	return nil
}

func (p *SAMLProvider) SupportsInteractive() bool { return true }
func (p *SAMLProvider) SupportsDirect() bool      { return false }

func (p *SAMLProvider) InitiateAuthentication(ctx context.Context, req *provider.InitiateRequest) (*provider.InitiateResponse, error) {
	// TODO: Implement SAML AuthnRequest generation
	// 1. Fetch IdP metadata from MetadataURL
	// 2. Generate signed AuthnRequest
	// 3. Build redirect URL using HTTP-Redirect binding
	return nil, fmt.Errorf("saml provider %q not yet implemented", p.config.ProviderID)
}

func (p *SAMLProvider) HandleCallback(ctx context.Context, req *provider.CallbackRequest) (*provider.AuthenticationResult, error) {
	// TODO: Implement SAML Response parsing
	// 1. Decode SAMLResponse (base64 + inflate)
	// 2. Verify signature using IdP certificate
	// 3. Extract NameID + attributes
	// 4. Validate conditions (NotBefore, NotOnOrAfter, Audience)
	return nil, fmt.Errorf("saml provider %q not yet implemented", p.config.ProviderID)
}

func (p *SAMLProvider) AuthenticateDirect(ctx context.Context, req *provider.DirectAuthRequest) (*provider.AuthenticationResult, error) {
	return nil, fmt.Errorf("saml does not support direct auth")
}
