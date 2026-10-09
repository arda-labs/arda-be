// Package finance is the gRPC client for finance-service PostingService.
// Callers: workflow-service workers (posting steps) and any internal flow
// that posts journal entries. Tenant scope travels through gRPC metadata —
// set ardametadata.Context on the outgoing context before calling.
package finance

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/client/retry"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

const defaultTimeout = 10 * time.Second

// PostingError is a typed business/policy error returned by finance-service.
// Cause retains the original gRPC status for callers that also inspect it.
type PostingError struct {
	Code  financev1.PostingErrorCode
	Cause error
}

// NewPostingError creates a typed finance business error at the client-side
// rule-resolution boundary, before a PostingService RPC is sent.
func NewPostingError(code financev1.PostingErrorCode, cause error) *PostingError {
	return &PostingError{Code: code, Cause: cause}
}

func (e *PostingError) Error() string {
	if e == nil || e.Cause == nil {
		return "finance posting error"
	}
	if st, ok := status.FromError(e.Cause); ok {
		return st.Message()
	}
	return e.Cause.Error()
}

func (e *PostingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// AsPostingError returns the finance error code when the server sent a
// recognized PostingError detail. Errors from older servers remain ordinary
// gRPC errors and can still be handled by their status/message.
func AsPostingError(err error) (*PostingError, bool) {
	var postingErr *PostingError
	if errors.As(err, &postingErr) {
		return postingErr, true
	}
	if err == nil {
		return nil, false
	}
	st, ok := status.FromError(err)
	if !ok {
		return nil, false
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.GetReason() != "POSTING_ERROR" {
			continue
		}
		codeValue, found := financev1.PostingErrorCode_value[info.GetMetadata()["posting_error_code"]]
		if !found || codeValue == int32(financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED) {
			continue
		}
		return &PostingError{Code: financev1.PostingErrorCode(codeValue), Cause: err}, true
	}
	return nil, false
}

func typedPostingError(err error) error {
	if typed, ok := AsPostingError(err); ok {
		return typed
	}
	return err
}

type Client struct {
	conn    *grpc.ClientConn
	api     financev1.PostingServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("finance grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("finance grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("finance-service")
	if err != nil {
		return nil, errors.New("finance grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		// Only read-only RPCs are retried; Post/Reserve/Release/Reverse writes
		// (and ValidatePosting's dry-run) must never be replayed as a mutation.
		retry.ReadOnly("arda.finance.v1.PostingService",
			"ValidatePosting", "GetJournalEntry", "ListPostingRules"),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "finance-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:    conn,
		api:     financev1.NewPostingServiceClient(conn),
		timeout: defaultTimeout,
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

// Validate resolves the posting without writing (FE preview path uses HTTP;
// this is for workers doing dry-run checks).
func (c *Client) Validate(ctx context.Context, req *financev1.PostingRequest) (*financev1.ValidationResult, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	result, err := c.api.ValidatePosting(callCtx, req)
	return result, typedPostingError(err)
}

// Post writes one balanced journal entry. Idempotent per idempotency_key.
// With a matching PENDING entry (created by Reserve) it converts that entry
// to POSTED; direct posts book straight to POSTED without a hold.
func (c *Client) Post(ctx context.Context, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.PostTransaction(callCtx, req)
	return resp, typedPostingError(err)
}

// Reserve creates the PENDING entry and holds its amounts on the account
// balances (two-phase balance: available drops at reserve, actual moves at
// Post). Idempotent per idempotency_key; editing the proposal releases the
// stale hold and re-reserves automatically.
func (c *Client) Reserve(ctx context.Context, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.ReservePosting(callCtx, req)
	return resp, typedPostingError(err)
}

// Release frees the reserved amounts of a PENDING entry and stamps it VOID
// (maker-checker reject path). Posted entries must be reversed instead.
func (c *Client) Release(ctx context.Context, req *financev1.ReleaseRequest) (*financev1.PostingResponse, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.ReleasePosting(callCtx, req)
	return resp, typedPostingError(err)
}

// Reverse creates the reversal entry for a posted journal entry.
func (c *Client) Reverse(ctx context.Context, req *financev1.ReverseRequest) (*financev1.PostingResponse, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.ReverseTransaction(callCtx, req)
	return resp, typedPostingError(err)
}

// GetJournalEntry reads one entry (header + lines) by entry_no — the
// cancellation flow's init/validate guards (status POSTED, not yet
// reversed). Unknown or non-readable entries surface as a NotFound gRPC
// error.
func (c *Client) GetJournalEntry(ctx context.Context, req *financev1.GetJournalEntryRequest) (*financev1.JournalEntryDetail, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.GetJournalEntry(callCtx, req)
}

// ListPostingRules exposes the fin_accounting_rules card for one document
// type so workers build their posting lines from config instead of
// hardcoded classification strings. Unseeded document types return an empty
// list — callers fall back to their built-in legs.
func (c *Client) ListPostingRules(ctx context.Context, documentType string) ([]*financev1.PostingRule, error) {
	if c == nil {
		return nil, errors.New("finance client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.ListPostingRules(callCtx, &financev1.ListPostingRulesRequest{DocumentType: documentType})
	if err != nil {
		return nil, err
	}
	return resp.GetRules(), nil
}
