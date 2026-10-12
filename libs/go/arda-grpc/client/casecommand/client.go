// Package casecommand is the gRPC client for arda.common.v1.CaseCommandService,
// the callback every domain service exposes for flows that run on the shared
// common-maker-checker process (ADR-011). One client targets one owner
// service; workflow-service keeps one client per owner.
package casecommand

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/client/retry"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

// Decision is the checker outcome ResolveCase applies.
type Decision string

const (
	DecisionApprove Decision = "APPROVE"
	DecisionReject  Decision = "REJECT"
)

func (d Decision) proto() (commonv1.CaseDecision, error) {
	switch d {
	case DecisionApprove:
		return commonv1.CaseDecision_CASE_DECISION_APPROVE, nil
	case DecisionReject:
		return commonv1.CaseDecision_CASE_DECISION_REJECT, nil
	default:
		return commonv1.CaseDecision_CASE_DECISION_UNSPECIFIED, fmt.Errorf("unsupported case decision %q", d)
	}
}

// Client calls CaseCommandService on one owner service.
type Client struct {
	conn    *grpc.ClientConn
	api     commonv1.CaseCommandServiceClient
	owner   string
	timeout time.Duration
}

// Dial connects to ownerService at addr with mTLS and a signed service
// identity from sourceService. ownerService is both the TLS server name and
// the identity audience, for example "hrm-service".
func Dial(addr, ownerService, sourceService string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	ownerService = strings.TrimSpace(ownerService)
	if addr == "" {
		return nil, fmt.Errorf("case command grpc address for %q is required", ownerService)
	}
	if ownerService == "" || strings.TrimSpace(sourceService) == "" {
		return nil, errors.New("case command client requires owner and source service names")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, fmt.Errorf("case command client for %s: service identity is not configured: %w", ownerService, err)
	}
	transportCreds, err := identity.ClientTransportCredentials(ownerService)
	if err != nil {
		return nil, fmt.Errorf("case command client for %s: tls is not configured: %w", ownerService, err)
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		// CheckCase is read-only and safe to retry; ResolveCase writes and is
		// retried by Zeebe, never by the transport.
		retry.ReadOnly("arda.common.v1.CaseCommandService", "CheckCase"),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, ownerService),
		),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return &Client{conn: conn, api: commonv1.NewCaseCommandServiceClient(conn), owner: ownerService, timeout: defaultTimeout}, nil
}

// Owner is the service this client targets.
func (c *Client) Owner() string {
	if c == nil {
		return ""
	}
	return c.owner
}

// CheckCase reports whether the business object passes its checks; message
// explains a failure to the maker.
func (c *Client) CheckCase(ctx context.Context, kind, objectID, caseID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("case command client is nil")
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(objectID) == "" {
		return false, "", errors.New("case kind and object id are required")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckCase(callCtx, &commonv1.CheckCaseRequest{Kind: kind, ObjectId: objectID, CaseId: caseID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

// ResolveCase applies decision and returns the variables to store on the
// process instance.
func (c *Client) ResolveCase(ctx context.Context, kind, objectID, caseID string, decision Decision, decidedBy, note string) (map[string]string, error) {
	if c == nil {
		return nil, errors.New("case command client is nil")
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(objectID) == "" {
		return nil, errors.New("case kind and object id are required")
	}
	value, err := decision.proto()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.ResolveCase(callCtx, &commonv1.ResolveCaseRequest{
		Kind: kind, ObjectId: objectID, CaseId: caseID, Decision: value, DecidedBy: decidedBy, Note: note,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetOutputs(), nil
}

// Close releases the connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// newWithConn builds a client on an existing connection; tests use it with an
// in-memory server.
func newWithConn(cc grpc.ClientConnInterface, ownerService string) *Client {
	return &Client{api: commonv1.NewCaseCommandServiceClient(cc), owner: ownerService, timeout: defaultTimeout}
}
