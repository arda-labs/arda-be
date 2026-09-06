package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
)

// LoanServer implements arda.loan.v1.LoanCommandService — the callback
// surface workflow-service workers call to execute loan steps. Tenant scope
// arrives via propagated gRPC metadata (X-Tenant-Id), same as CRM.
type LoanServer struct {
	loanv1.UnimplementedLoanCommandServiceServer
	contracts *service.LoanService
	adj       *service.AdjustmentService
}

func NewLoanServer(contracts *service.LoanService, adj *service.AdjustmentService) *LoanServer {
	return &LoanServer{contracts: contracts, adj: adj}
}

func tenantFromContext(ctx context.Context) (string, error) {
	md := ardametadata.FromIncoming(ctx)
	tenantID := strings.TrimSpace(md.TenantID)
	if tenantID == "" {
		return "", fmt.Errorf("tenant scope is required")
	}
	return tenantID, nil
}

func (s *LoanServer) UpdateContractStatus(ctx context.Context, req *loanv1.UpdateContractStatusRequest) (*loanv1.UpdateContractStatusResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if req.GetContractId() == "" || req.GetStatus() == "" {
		return nil, status.Error(codes.InvalidArgument, "contract_id and status are required")
	}
	if err := s.contracts.SetContractStatus(ctx, tenantID, req.GetContractId(), req.GetStatus()); err != nil {
		slog.Warn("loan grpc: update contract status failed", "contractId", req.GetContractId(), "err", err)
		return &loanv1.UpdateContractStatusResponse{Ok: false}, nil
	}
	return &loanv1.UpdateContractStatusResponse{Ok: true}, nil
}

func (s *LoanServer) CheckAdjustment(ctx context.Context, req *loanv1.CheckAdjustmentRequest) (*loanv1.CheckAdjustmentResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.adj.Check(ctx, req.GetKind(), tenantID, req.GetAdjustmentId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &loanv1.CheckAdjustmentResponse{Ok: ok, Message: message}, nil
}

func (s *LoanServer) ResolveAdjustment(ctx context.Context, req *loanv1.ResolveAdjustmentRequest) (*loanv1.ResolveAdjustmentResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if req.GetAdjustmentId() == "" || req.GetDecision() == "" {
		return nil, status.Error(codes.InvalidArgument, "adjustment_id and decision are required")
	}
	if err := s.adj.Resolve(ctx, req.GetKind(), tenantID, req.GetAdjustmentId(),
		req.GetDecision(), req.GetDecidedBy(), req.GetNote()); err != nil {
		slog.Warn("loan grpc: resolve adjustment failed", "kind", req.GetKind(), "id", req.GetAdjustmentId(), "err", err)
		return &loanv1.ResolveAdjustmentResponse{Ok: false}, nil
	}
	return &loanv1.ResolveAdjustmentResponse{Ok: true}, nil
}
