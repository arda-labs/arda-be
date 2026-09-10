// Package deposit is the gRPC client for deposit-service DepositCommandService.
package deposit

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	depositv1 "github.com/arda-labs/arda/libs/go/arda-proto/deposit/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

// Settler is the narrow surface the workflow deposit workers need.
type Settler interface {
	CheckSettle(ctx context.Context, savingsCode string) (bool, string, error)
	Settle(ctx context.Context, savingsCode, actor string) error
}

type Client struct {
	conn    *grpc.ClientConn
	api     depositv1.DepositCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("deposit grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("deposit grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("deposit-service")
	if err != nil {
		return nil, errors.New("deposit grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "deposit-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, api: depositv1.NewDepositCommandServiceClient(conn), timeout: defaultTimeout}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) CheckSettle(ctx context.Context, savingsCode string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckSettle(callCtx, &depositv1.CheckSettleRequest{SavingsCode: savingsCode})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) Settle(ctx context.Context, savingsCode, actor string) error {
	if c == nil {
		return errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.Settle(callCtx, &depositv1.SettleRequest{SavingsCode: savingsCode, Actor: actor})
	return err
}

// Additionaler is the narrow surface the workflow additional-deposit workers
// need (DPM.301).
type Additionaler interface {
	CheckAdditional(ctx context.Context, savingsCode string, amountMinor int64) (bool, string, error)
	SettleAdditional(ctx context.Context, savingsCode string, amountMinor int64, txnDate, idempotencyKey, actor string) error
}

func (c *Client) CheckAdditional(ctx context.Context, savingsCode string, amountMinor int64) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckAdditional(callCtx, &depositv1.CheckAdditionalRequest{
		SavingsCode: savingsCode,
		AmountMinor: amountMinor,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) SettleAdditional(ctx context.Context, savingsCode string, amountMinor int64, txnDate, idempotencyKey, actor string) error {
	if c == nil {
		return errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.SettleAdditional(callCtx, &depositv1.SettleAdditionalRequest{
		SavingsCode:    savingsCode,
		AmountMinor:    amountMinor,
		TxnDate:        txnDate,
		IdempotencyKey: idempotencyKey,
		Actor:          actor,
	})
	return err
}

// ProductRequester is the narrow surface the workflow product-request workers
// need (DPM.102/103).
type ProductRequester interface {
	CheckProductRequest(ctx context.Context, requestID string) (bool, string, error)
	ResolveProductRequest(ctx context.Context, requestID, decision, actor, note string) error
}

func (c *Client) CheckProductRequest(ctx context.Context, requestID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckProductRequest(callCtx, &depositv1.CheckProductRequestRequest{
		ProductRequestId: requestID,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) ResolveProductRequest(ctx context.Context, requestID, decision, actor, note string) error {
	if c == nil {
		return errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveProductRequest(callCtx, &depositv1.ResolveProductRequestRequest{
		ProductRequestId: requestID,
		Decision:         decision,
		Actor:            actor,
		Note:             note,
	})
	return err
}

// IBMRequester is the narrow surface the workflow IBM workers need
// (IBM.200/300/301/302/304).
type IBMRequester interface {
	CheckIBMRequest(ctx context.Context, kind, refID string) (bool, string, error)
	ResolveIBMRequest(ctx context.Context, kind, refID, decision, actor string) error
}

func (c *Client) CheckIBMRequest(ctx context.Context, kind, refID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckIBMRequest(callCtx, &depositv1.CheckIBMRequestRequest{Kind: kind, RefId: refID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) ResolveIBMRequest(ctx context.Context, kind, refID, decision, actor string) error {
	if c == nil {
		return errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveIBMRequest(callCtx, &depositv1.ResolveIBMRequestRequest{
		Kind:     kind,
		RefId:    refID,
		Decision: decision,
		Actor:    actor,
	})
	return err
}

// RateRequester is the narrow surface the workflow rate workers need
// (DPM.100/101).
type RateRequester interface {
	CheckRateRequest(ctx context.Context, requestID string) (bool, string, error)
	ResolveRateRequest(ctx context.Context, requestID, decision, actor string) error
}

func (c *Client) CheckRateRequest(ctx context.Context, requestID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckRateRequest(callCtx, &depositv1.CheckRateRequestRequest{RequestId: requestID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) ResolveRateRequest(ctx context.Context, requestID, decision, actor string) error {
	if c == nil {
		return errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveRateRequest(callCtx, &depositv1.ResolveRateRequestRequest{
		RequestId: requestID,
		Decision:  decision,
		Actor:     actor,
	})
	return err
}

// InterestOperator is the narrow surface the workflow interest workers need
// (DPM.302/303/304).
type InterestOperator interface {
	CheckInterestOp(ctx context.Context, opID string) (bool, string, error)
	ResolveInterestOp(ctx context.Context, opID, decision, actor string) error
}

func (c *Client) CheckInterestOp(ctx context.Context, opID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckInterestOp(callCtx, &depositv1.CheckInterestOpRequest{OpId: opID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) ResolveInterestOp(ctx context.Context, opID, decision, actor string) error {
	if c == nil {
		return errors.New("deposit client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveInterestOp(callCtx, &depositv1.ResolveInterestOpRequest{
		OpId:     opID,
		Decision: decision,
		Actor:    actor,
	})
	return err
}
