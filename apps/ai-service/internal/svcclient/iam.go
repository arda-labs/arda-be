package svcclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// UserSummary is the redacted directory user shape exposed to the AI SDK.
type UserSummary struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles"`
}

// UserListPage is a redacted page of directory users.
type UserListPage struct {
	Items   []UserSummary `json:"items"`
	Total   int           `json:"total"`
	Page    int           `json:"page"`
	PerPage int           `json:"per_page"`
}

// ListUsersParams controls the /internal/ai/users read.
type ListUsersParams struct {
	Search string
	Status string
	Limit  int
	Page   int
}

// IAMClient calls the IAM service internal surface (/internal/ai/*).
type IAMClient struct {
	*Client
}

// NewIAMClient returns a typed client for the IAM service.
func NewIAMClient(baseURL, source, secret string, hc *http.Client) *IAMClient {
	return &IAMClient{Client: NewClient("iam-service", baseURL, source, secret, hc)}
}

// ListUsers lists directory users in the delegated tenant with redacted
// fields (id/username/email/name/status/roles). Admin-only behind
// iam.user.read at the dispatcher.
func (c *IAMClient) ListUsers(ctx context.Context, md metadata.Context, params ListUsersParams) (UserListPage, error) {
	var zero UserListPage
	search := strings.TrimSpace(params.Search)
	if len(search) > 128 {
		return zero, fmt.Errorf("search is too long (max 128 characters)")
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}
	status := strings.TrimSpace(strings.ToUpper(params.Status))

	q := url.Values{}
	q.Set("tenant_id", md.TenantID)
	q.Set("page", strconv.Itoa(page))
	q.Set("size", strconv.Itoa(limit))
	if search != "" {
		q.Set("q", search)
	}
	if status != "" {
		q.Set("status", status)
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/internal/ai/users?"+q.Encode(), md)
	if err != nil {
		return zero, err
	}
	var envelope struct {
		Result UserListPage `json:"result"`
	}
	if err := c.Do(ctx, req, &envelope); err != nil {
		return zero, err
	}
	return envelope.Result, nil
}
