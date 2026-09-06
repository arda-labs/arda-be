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
	contracts     *service.LoanService
	adj           *service.AdjustmentService
	disbursements *service.DisbursementService
}

func NewLoanServer(contracts *service.LoanService, adj *service.AdjustmentService, disbursements *service.DisbursementService) *LoanServer {
	return &LoanServer{contracts: contracts, adj: adj, disbursements: disbursements}
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

// ── Disbursement flow (P1b, LNM.300.02) ──

func (s *LoanServer) CheckDisbursement(ctx context.Context, req *loanv1.CheckDisbursementRequest) (*loanv1.CheckDisbursementResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.disbursements.Check(ctx, tenantID, req.GetDisbursementId())
	if err != nil {
		return &loanv1.CheckDisbursementResponse{Ok: false, Message: err.Error()}, nil
	}
	return &loanv1.CheckDisbursementResponse{Ok: ok, Message: message}, nil
}

func (s *LoanServer) GetDisbursementPostingDetail(ctx context.Context, req *loanv1.GetDisbursementPostingDetailRequest) (*loanv1.DisbursementPostingDetail, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	detail, err := s.disbursements.PostingDetail(ctx, tenantID, req.GetDisbursementId())
	if err != nil {
		slog.Warn("loan grpc: posting detail failed", "id", req.GetDisbursementId(), "err", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return detail, nil
}

func (s *LoanServer) SettleDisbursement(ctx context.Context, req *loanv1.SettleDisbursementRequest) (*loanv1.SettleDisbursementResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.disbursements.Settle(ctx, tenantID, req.GetDisbursementId(), req.GetJournalEntryId(), req.GetActor()); err != nil {
		slog.Warn("loan grpc: settle failed", "id", req.GetDisbursementId(), "err", err)
		return &loanv1.SettleDisbursementResponse{Ok: false}, nil
	}
	return &loanv1.SettleDisbursementResponse{Ok: true}, nil
}

func (s *LoanServer) ResolveDisbursement(ctx context.Context, req *loanv1.ResolveDisbursementRequest) (*loanv1.ResolveDisbursementResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.disbursements.Resolve(ctx, tenantID, req.GetDisbursementId(), req.GetDecision(), req.GetDecidedBy(), req.GetNote()); err != nil {
		slog.Warn("loan grpc: resolve disbursement failed", "id", req.GetDisbursementId(), "err", err)
		return &loanv1.ResolveDisbursementResponse{Ok: false}, nil
	}
	return &loanv1.ResolveDisbursementResponse{Ok: true}, nil
}
