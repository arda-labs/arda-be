package svcclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// Account is a chart-of-accounts entry exposed to the AI SDK.
type Account struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	NormalBalance string `json:"normalBalance"`
	Currency      string `json:"currency"`
	IsActive      bool   `json:"isActive"`
}

// GetAccountResult pairs an account with its current balance.
type GetAccountResult struct {
	Account Account `json:"account"`
	Balance any     `json:"balance"`
}

// FinanceClient calls the finance service internal surface (/internal/ai/*).
type FinanceClient struct {
	*Client
}

// NewFinanceClient returns a typed client for the finance service.
func NewFinanceClient(baseURL, source, secret string, hc *http.Client) *FinanceClient {
	return &FinanceClient{Client: NewClient("finance-service", baseURL, source, secret, hc)}
}

// GetAccount reads a chart-of-accounts entry in the delegated tenant.
func (c *FinanceClient) GetAccount(ctx context.Context, md metadata.Context, accountID string) (GetAccountResult, error) {
	var zero GetAccountResult
	if strings.TrimSpace(accountID) == "" || len(accountID) > 128 {
		return zero, fmt.Errorf("accountId is required (max 128 characters)")
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/internal/ai/accounts/"+url.PathEscape(accountID), md)
	if err != nil {
		return zero, err
	}
	var envelope struct {
		Result GetAccountResult `json:"result"`
	}
	if err := c.Do(ctx, req, &envelope); err != nil {
		return zero, err
	}
	return envelope.Result, nil
}
