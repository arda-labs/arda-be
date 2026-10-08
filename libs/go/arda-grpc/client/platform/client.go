package platform

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
	"github.com/arda-labs/arda/libs/go/arda-grpc/client/retry"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 2 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     platformv1.PlatformServiceClient
	timeout time.Duration
	logger  *slog.Logger
}

type Scope struct {
	TenantID  string
	ScopeType string
	ScopeID   string
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("platform grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("platform grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("platform-service")
	if err != nil {
		return nil, errors.New("platform grpc tls is not configured: " + err.Error())
	}
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		// Only read-only RPCs are retried; ResolveParameter reads a setting.
		retry.ReadOnly("arda.platform.v1.PlatformService", "ResolveParameter", "ListLookupValues", "GetBusinessDate", "IsWorkingDay"),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "platform-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:    conn,
		api:     platformv1.NewPlatformServiceClient(conn),
		timeout: defaultTimeout,
		logger:  logger,
	}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) ResolveString(ctx context.Context, tenantID, key string) (string, error) {
	return c.ResolveStringWithScopes(ctx, tenantID, key)
}

func (c *Client) ResolveStringWithScopes(ctx context.Context, tenantID, key string, scopes ...Scope) (string, error) {
	response, err := c.ResolveParameter(ctx, &platformv1.ResolveParameterRequest{
		TenantId:      tenantID,
		Module:        moduleForKey(key),
		Key:           key,
		EffectiveDate: time.Now().UTC().Format("2006-01-02"),
		Scopes:        scopeSelectors(scopes),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(response.GetValue()), nil
}

func (c *Client) ResolveParameter(ctx context.Context, request *platformv1.ResolveParameterRequest) (*platformv1.Parameter, error) {
	if c == nil {
		return nil, errors.New("platform client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if request == nil {
		return nil, errors.New("platform parameter resolve request is nil")
	}
	resp, err := c.api.ResolveParameter(callCtx, request)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListLookupValues(ctx context.Context, tenantID, categoryCode string) (*platformv1.ListLookupValuesResponse, error) {
	if c == nil {
		return nil, errors.New("platform client is nil")
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(categoryCode) == "" {
		return nil, errors.New("platform lookup tenant and category are required")
	}
	outgoing := ardametadata.FromOutgoing(ctx)
	if outgoing.TenantID != "" && outgoing.TenantID != tenantID {
		return nil, errors.New("platform lookup tenant is outside outgoing context scope")
	}
	if outgoing.TenantID == "" {
		ctx = ardametadata.AppendToOutgoing(ctx, ardametadata.Context{TenantID: tenantID})
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.ListLookupValues(callCtx, &platformv1.ListLookupValuesRequest{CategoryCode: categoryCode})
}

func (c *Client) CurrentBusinessDate(ctx context.Context, scope ardaBusinessDate.Scope) (time.Time, error) {
	if c == nil {
		return time.Time{}, errors.New("platform client is nil")
	}
	ctx, tenantID, err := businessDateContext(ctx, scope)
	if err != nil {
		return time.Time{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.api.GetBusinessDate(callCtx, &platformv1.GetBusinessDateRequest{Scope: &platformv1.ScopeSelector{TenantId: tenantID, ScopeType: string(scope.Type), ScopeId: scope.OrgCode}})
	if err != nil {
		return time.Time{}, err
	}
	date, err := time.Parse("2006-01-02", response.GetBusinessDate())
	if err != nil {
		return time.Time{}, errors.New("Platform returned an invalid business date")
	}
	return date, nil
}

func (c *Client) IsHoliday(ctx context.Context, scope ardaBusinessDate.Scope, date time.Time) (bool, error) {
	if c == nil {
		return false, errors.New("platform client is nil")
	}
	ctx, tenantID, err := businessDateContext(ctx, scope)
	if err != nil {
		return false, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.api.IsWorkingDay(callCtx, &platformv1.IsWorkingDayRequest{
		Scope: &platformv1.ScopeSelector{TenantId: tenantID, ScopeType: string(scope.Type), ScopeId: scope.OrgCode},
		Date:  date.Format("2006-01-02"),
	})
	if err != nil {
		return false, err
	}
	return !response.GetIsWorkingDay(), nil
}

func businessDateContext(ctx context.Context, scope ardaBusinessDate.Scope) (context.Context, string, error) {
	tenantID := strings.TrimSpace(scope.TenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(ardametadata.FromOutgoing(ctx).TenantID)
	}
	if tenantID == "" {
		return ctx, "", errors.New("Platform business-date RPC requires a verified tenant context")
	}
	if existing := ardametadata.FromOutgoing(ctx).TenantID; existing != "" && existing != tenantID {
		return ctx, "", errors.New("Platform business-date tenant is outside outgoing context scope")
	}
	if ardametadata.FromOutgoing(ctx).TenantID == "" {
		ctx = ardametadata.AppendToOutgoing(ctx, ardametadata.Context{TenantID: tenantID})
	}
	return ctx, tenantID, nil
}

func scopeSelectors(scopes []Scope) []*platformv1.ScopeSelector {
	selectors := make([]*platformv1.ScopeSelector, 0, len(scopes))
	for _, scope := range scopes {
		selectors = append(selectors, &platformv1.ScopeSelector{
			TenantId: scope.TenantID, ScopeType: scope.ScopeType, ScopeId: scope.ScopeID,
		})
	}
	return selectors
}

func moduleForKey(key string) string {
	for _, prefix := range []struct{ keyPrefix, module string }{
		{"LNM_", "loan"}, {"DPM_", "deposit"}, {"FIN_", "finance"},
		{"CAP_", "capital"}, {"CRM_", "crm"}, {"IAM_", "iam"},
		{"PLT_", "platform"}, {"PLATFORM_", "platform"},
	} {
		if strings.HasPrefix(key, prefix.keyPrefix) {
			return prefix.module
		}
	}
	return "platform"
}
