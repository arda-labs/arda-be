package grpc

import (
	"context"

	"github.com/arda-labs/arda/apps/capital-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	capitalv1 "github.com/arda-labs/arda/libs/go/arda-proto/capital/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CapitalServer implements arda.capital.v1.CapitalCommandService — the callback
// surface workflow-service workers use for cfc-contract/amendment/movement.
type CapitalServer struct {
	capitalv1.UnimplementedCapitalCommandServiceServer
	svc *service.CapitalService
}

func NewCapitalServer(svc *service.CapitalService) *CapitalServer {
	return &CapitalServer{svc: svc}
}

func tenantFromContext(ctx context.Context) (string, error) {
	tenantID := ardametadata.FromIncoming(ctx).TenantID
	if tenantID == "" {
		return "", status.Error(codes.PermissionDenied, "tenant scope is required")
	}
	return tenantID, nil
}

func (s *CapitalServer) CheckRequest(ctx context.Context, req *capitalv1.CheckRequestRequest) (*capitalv1.CheckRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	ok, message, err := s.svc.CheckRequest(ctx, tenantID, req.GetKind(), req.GetRefId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &capitalv1.CheckRequestResponse{Ok: ok, Message: message}, nil
}

func (s *CapitalServer) ResolveRequest(ctx context.Context, req *capitalv1.ResolveRequestRequest) (*capitalv1.ResolveRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.svc.ResolveRequest(ctx, tenantID, req.GetKind(), req.GetRefId(), req.GetDecision(), req.GetActor()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &capitalv1.ResolveRequestResponse{Ok: true}, nil
}
