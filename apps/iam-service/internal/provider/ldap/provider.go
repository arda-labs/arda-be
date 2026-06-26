package ldap

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/provider"
)

// Config holds LDAP identity provider configuration.
type Config struct {
	ProviderID    string   `yaml:"id"`
	ProviderName  string   `yaml:"name"`
	ServerURL     string   `yaml:"server_url"`
	BindDN        string   `yaml:"bind_dn"`
	BindPassword  string   `yaml:"bind_password"`
	BaseDN        string   `yaml:"base_dn"`
	UserFilter    string   `yaml:"user_filter"`
	UserBaseDN    string   `yaml:"user_base_dn"`
	Attributes    []string `yaml:"attributes"`
	Domains       []string `yaml:"domains"`
	Priority      int      `yaml:"priority"`
}

// LDAPProvider authenticates users via LDAP/Active Directory.
type LDAPProvider struct {
	config Config
}

// New creates an LDAP provider. Ready for implementation.
func New(cfg Config) *LDAPProvider {
	return &LDAPProvider{config: cfg}
}

func (p *LDAPProvider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          p.config.ProviderID,
		Type:        provider.TypeLDAP,
		Name:        p.config.ProviderName,
		Description: fmt.Sprintf("Đăng nhập bằng %s", p.config.ProviderName),
		Priority:    p.config.Priority,
		IsEnabled:   true,
		Domains:     p.config.Domains,
	}
}

func (p *LDAPProvider) Validate(ctx context.Context) error {
	if p.config.ServerURL == "" {
		return fmt.Errorf("ldap %q: server_url required", p.config.ProviderID)
	}
	return nil
}

func (p *LDAPProvider) SupportsInteractive() bool { return false }
func (p *LDAPProvider) SupportsDirect() bool      { return true }

func (p *LDAPProvider) InitiateAuthentication(ctx context.Context, req *provider.InitiateRequest) (*provider.InitiateResponse, error) {
	return nil, fmt.Errorf("ldap does not support interactive auth")
}

func (p *LDAPProvider) HandleCallback(ctx context.Context, req *provider.CallbackRequest) (*provider.AuthenticationResult, error) {
	return nil, fmt.Errorf("ldap does not support callbacks")
}

func (p *LDAPProvider) AuthenticateDirect(ctx context.Context, req *provider.DirectAuthRequest) (*provider.AuthenticationResult, error) {
	// TODO: Implement LDAP bind authentication
	// 1. Connect to LDAP server (ldaps://)
	// 2. Bind with service account (BindDN/BindPassword)
	// 3. Search for user using UserFilter
	// 4. Re-bind with user DN + password to verify credentials
	// 5. Extract user attributes (username, email, displayName, groups)
	// 6. Return AuthenticationResult matching the interface
	return nil, fmt.Errorf("ldap provider %q not yet implemented", p.config.ProviderID)
}
