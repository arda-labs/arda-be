package workflow

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const defaultTimeout = 5 * time.Second

type Client struct {
	conn    *grpc.ClientConn
	api     workflowv1.WorkflowCommandServiceClient
	timeout time.Duration
}

type CaseCreate struct {
	TenantID          string
	CaseType          string
	CaseCode          string
	Title             string
	PrimaryObjectType string
	PrimaryObjectID   string
	DomainService     string
	Priority          string
	CreatedBy         string
	IdempotencyKey    string
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("workflow grpc address is required")
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{})),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:    conn,
		api:     workflowv1.NewWorkflowCommandServiceClient(conn),
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

func (c *Client) CreateCase(ctx context.Context, in CaseCreate) (*workflowv1.BusinessCase, error) {
	if c == nil {
		return nil, errors.New("workflow client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.CreateCase(callCtx, &workflowv1.CreateCaseRequest{
		TenantId:          in.TenantID,
		CaseType:          in.CaseType,
		CaseCode:          in.CaseCode,
		Title:             in.Title,
		PrimaryObjectType: in.PrimaryObjectType,
		PrimaryObjectId:   in.PrimaryObjectID,
		DomainService:     in.DomainService,
		Priority:          in.Priority,
		CreatedBy:         in.CreatedBy,
		IdempotencyKey:    in.IdempotencyKey,
	})
}

func (c *Client) SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any) (*workflowv1.BusinessCase, error) {
	if c == nil {
		return nil, errors.New("workflow client is nil")
	}
	var vars *structpb.Struct
	if len(variables) > 0 {
		var builtVars, err = structpb.NewStruct(variables)
		if err != nil {
			return nil, err
		}
		vars = builtVars
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.SubmitCase(callCtx, &workflowv1.SubmitCaseRequest{
		CaseId:    caseID,
		Actor:     actor,
		Variables: vars,
	})
}
