package grpc

import (
	"context"
	"errors"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	"github.com/arda-labs/arda/apps/crm-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	crmv1 "github.com/arda-labs/arda/libs/go/arda-proto/crm/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MemberCommandServer is the callback surface for the CRM_MEMBER_V1 flow: the
// checker decision arrives here from workflow-service workers.
type MemberCommandServer struct {
	crmv1.UnimplementedMemberCommandServiceServer
	svc *service.MemberService
}

func NewMemberCommandServer(svc *service.MemberService) *MemberCommandServer {
	return &MemberCommandServer{svc: svc}
}

// CheckMemberRequest validates that the staged request may be approved.
func (s *MemberCommandServer) CheckMemberRequest(ctx context.Context, req *crmv1.CheckMemberRequestRequest) (*crmv1.CheckMemberRequestResponse, error) {
	tenantID, err := memberTenant(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if strings.TrimSpace(req.GetRequestId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id is required")
	}
	ok, message, err := s.svc.CheckCapitalRequest(ctx, tenantID, req.GetRequestId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &crmv1.CheckMemberRequestResponse{Ok: ok, Message: message}, nil
}

// ResolveMemberRequest records the decision (APPROVE moves the stake).
func (s *MemberCommandServer) ResolveMemberRequest(ctx context.Context, req *crmv1.ResolveMemberRequestRequest) (*crmv1.ResolveMemberRequestResponse, error) {
	tenantID, err := memberTenant(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if strings.TrimSpace(req.GetRequestId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id is required")
	}
	decision := strings.ToUpper(strings.TrimSpace(req.GetDecision()))
	switch decision {
	case "APPROVE":
		decision = "APPROVED"
	case "REJECT":
		decision = "REJECTED"
	}
	if decision != "APPROVED" && decision != "REJECTED" {
		return nil, status.Error(codes.InvalidArgument, "decision must be APPROVE or REJECT")
	}
	if _, _, err := s.svc.ResolveCapitalRequest(ctx, tenantID, req.GetRequestId(), decision, req.GetActor(), req.GetDataVersion()); err != nil {
		// Only genuine domain conflicts are terminal for the caller: Aborted is
		// the stale optimistic-lock token and FailedPrecondition means the
		// decision cannot apply (unknown request, inactive member, over-draw).
		// Infrastructure failures must stay retryable, so they surface as
		// Internal instead of being mistaken for a permanent conflict.
		switch {
		case errors.Is(err, repository.ErrMemberVersionConflict):
			return nil, status.Error(codes.Aborted, err.Error())
		case errors.Is(err, repository.ErrMemberRequestNotFound),
			errors.Is(err, repository.ErrMemberNotActive),
			errors.Is(err, repository.ErrMemberInsufficientCapital):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	return &crmv1.ResolveMemberRequestResponse{Ok: true}, nil
}

// memberTenant reads the delegated tenant from the verified gRPC metadata.
func memberTenant(ctx context.Context) (string, error) {
	md := ardametadata.FromIncoming(ctx)
	tenantID := strings.TrimSpace(md.TenantID)
	if tenantID == "" {
		return "", status.Error(codes.PermissionDenied, "tenant scope is required")
	}
	return tenantID, nil
}
