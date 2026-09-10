package grpc

import (
	"context"

	"github.com/arda-labs/arda/apps/statistical-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	statisticalv1 "github.com/arda-labs/arda/libs/go/arda-proto/statistical/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StatisticalServer implements arda.statistical.v1.StatisticalCommandService —
// the callback surface workflow-service workers use for rpt-submit-v2.
type StatisticalServer struct {
	statisticalv1.UnimplementedStatisticalCommandServiceServer
	svc *service.StatisticalService
}

func NewStatisticalServer(svc *service.StatisticalService) *StatisticalServer {
	return &StatisticalServer{svc: svc}
}

func tenantFromContext(ctx context.Context) (string, error) {
	md := ardametadata.FromIncoming(ctx).TenantID
	if md == "" {
		return "", status.Error(codes.PermissionDenied, "tenant scope is required")
	}
	return md, nil
}

func (s *StatisticalServer) CheckSubmission(ctx context.Context, req *statisticalv1.CheckSubmissionRequest) (*statisticalv1.CheckSubmissionResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	ok, message, err := s.svc.CheckSubmission(ctx, tenantID, req.GetSubmissionId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &statisticalv1.CheckSubmissionResponse{Ok: ok, Message: message}, nil
}

func (s *StatisticalServer) ResolveSubmission(ctx context.Context, req *statisticalv1.ResolveSubmissionRequest) (*statisticalv1.ResolveSubmissionResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.svc.ResolveSubmission(ctx, tenantID, req.GetSubmissionId(), req.GetDecision(), req.GetActor()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &statisticalv1.ResolveSubmissionResponse{Ok: true}, nil
}
