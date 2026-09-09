package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
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
	collections   *service.CollectionService
	disbBatches   *service.BatchDisbursementService
	colBatches    *service.BatchCollectionService
}

func NewLoanServer(contracts *service.LoanService, adj *service.AdjustmentService, disbursements *service.DisbursementService, collections *service.CollectionService, disbBatches *service.BatchDisbursementService, colBatches *service.BatchCollectionService) *LoanServer {
	return &LoanServer{contracts: contracts, adj: adj, disbursements: disbursements, collections: collections, disbBatches: disbBatches, colBatches: colBatches}
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

func (s *LoanServer) GetContract(ctx context.Context, req *loanv1.GetContractRequest) (*loanv1.ContractBrief, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	contract, err := s.contracts.GetContract(ctx, tenantID, req.GetContractId())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	brief := &loanv1.ContractBrief{
		ContractId:   contract.ID,
		ContractCode: contract.ContractCode,
		CustomerCode: contract.CustomerCode,
		Status:       contract.Status,
		LoanAmtMinor: contract.LoanAmt,
	}
	if contract.WorkflowCaseID != nil {
		brief.WorkflowCaseId = *contract.WorkflowCaseID
	}
	return brief, nil
}

// CheckFormation validates the contract is actionable for the
// LOAN_FORMATION_V2 workflow (BPMN validate job): it must exist and sit in
// the submitted-but-not-approved PENDING state.
func (s *LoanServer) CheckFormation(ctx context.Context, req *loanv1.CheckFormationRequest) (*loanv1.CheckFormationResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	contract, err := s.contracts.GetContract(ctx, tenantID, req.GetContractId())
	if err != nil {
		return &loanv1.CheckFormationResponse{Ok: false, Message: err.Error()}, nil
	}
	if contract.Status != domain.ContractPending {
		return &loanv1.CheckFormationResponse{
			Ok:      false,
			Message: fmt.Sprintf("status %s is not actionable", contract.Status),
		}, nil
	}
	return &loanv1.CheckFormationResponse{Ok: true}, nil
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


// ── Collection flow (P1b.4, LNM.301.02) ──

func (s *LoanServer) CheckCollection(ctx context.Context, req *loanv1.CheckCollectionRequest) (*loanv1.CheckCollectionResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.collections.Check(ctx, tenantID, req.GetCollectionId())
	if err != nil {
		return &loanv1.CheckCollectionResponse{Ok: false, Message: err.Error()}, nil
	}
	return &loanv1.CheckCollectionResponse{Ok: ok, Message: message}, nil
}

func (s *LoanServer) GetCollectionPostingDetail(ctx context.Context, req *loanv1.GetCollectionPostingDetailRequest) (*loanv1.CollectionPostingDetail, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	detail, err := s.collections.PostingDetail(ctx, tenantID, req.GetCollectionId())
	if err != nil {
		slog.Warn("loan grpc: collection detail failed", "id", req.GetCollectionId(), "err", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return detail, nil
}

func (s *LoanServer) SettleCollection(ctx context.Context, req *loanv1.SettleCollectionRequest) (*loanv1.SettleCollectionResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.collections.Settle(ctx, tenantID, req.GetCollectionId(), req.GetJournalEntryId(), req.GetActor()); err != nil {
		slog.Warn("loan grpc: settle collection failed", "id", req.GetCollectionId(), "err", err)
		return &loanv1.SettleCollectionResponse{Ok: false}, nil
	}
	return &loanv1.SettleCollectionResponse{Ok: true}, nil
}

func (s *LoanServer) ResolveCollection(ctx context.Context, req *loanv1.ResolveCollectionRequest) (*loanv1.ResolveCollectionResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.collections.Resolve(ctx, tenantID, req.GetCollectionId(), req.GetDecision(), req.GetDecidedBy(), req.GetNote()); err != nil {
		slog.Warn("loan grpc: resolve collection failed", "id", req.GetCollectionId(), "err", err)
		return &loanv1.ResolveCollectionResponse{Ok: false}, nil
	}
	return &loanv1.ResolveCollectionResponse{Ok: true}, nil
}

// ── Batch flows (iteration 13, 1 hồ sơ — N hợp đồng) ──

// batchTypeByCase variables name the batch kind; when absent the batch_type
// request field selects the service. Defaults to the disbursement register
// leg for backward safety with older workers.
func (s *LoanServer) batchDisbSvc() *service.BatchDisbursementService { return s.disbBatches }
func (s *LoanServer) batchColSvc() *service.BatchCollectionService   { return s.colBatches }

func isCollectionBatchType(batchType string) bool {
	return batchType == domain.BatchTypeCollection
}

func (s *LoanServer) GetBatchPostingDetail(ctx context.Context, req *loanv1.GetBatchRequest) (*loanv1.BatchPostingDetail, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if req.GetBatchId() == "" {
		return nil, status.Error(codes.InvalidArgument, "batch_id is required")
	}
	if isCollectionBatchType(req.GetBatchType()) {
		detail, err := s.batchColSvc().BatchPostingDetail(ctx, tenantID, req.GetBatchId())
		if err != nil {
			slog.Warn("loan grpc: batch posting detail failed", "id", req.GetBatchId(), "err", err)
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return detail, nil
	}
	detail, err := s.batchDisbSvc().BatchPostingDetail(ctx, tenantID, req.GetBatchId())
	if err != nil {
		slog.Warn("loan grpc: batch posting detail failed", "id", req.GetBatchId(), "err", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return detail, nil
}

func (s *LoanServer) CheckBatch(ctx context.Context, req *loanv1.CheckBatchRequest) (*loanv1.CheckBatchResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if req.GetBatchId() == "" {
		return nil, status.Error(codes.InvalidArgument, "batch_id is required")
	}
	if isCollectionBatchType(req.GetBatchType()) {
		ok, message, err := s.batchColSvc().Check(ctx, tenantID, req.GetBatchId())
		if err != nil {
			return &loanv1.CheckBatchResponse{Ok: false, Message: err.Error()}, nil
		}
		return &loanv1.CheckBatchResponse{Ok: ok, Message: message}, nil
	}
	ok, message, err := s.batchDisbSvc().Check(ctx, tenantID, req.GetBatchId())
	if err != nil {
		return &loanv1.CheckBatchResponse{Ok: false, Message: err.Error()}, nil
	}
	return &loanv1.CheckBatchResponse{Ok: ok, Message: message}, nil
}

func (s *LoanServer) SettleBatch(ctx context.Context, req *loanv1.SettleBatchRequest) (*loanv1.SettleBatchResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if req.GetBatchId() == "" {
		return nil, status.Error(codes.InvalidArgument, "batch_id is required")
	}
	if isCollectionBatchType(req.GetBatchType()) {
		if err := s.batchColSvc().SettleBatchCollection(ctx, tenantID, req.GetBatchId(), req.GetJournalEntryId(), req.GetActor()); err != nil {
			slog.Warn("loan grpc: settle collection batch failed", "id", req.GetBatchId(), "err", err)
			return &loanv1.SettleBatchResponse{Ok: false}, nil
		}
		return &loanv1.SettleBatchResponse{Ok: true}, nil
	}
	if err := s.batchDisbSvc().SettleBatch(ctx, tenantID, req.GetBatchId(), req.GetJournalEntryId(), req.GetActor()); err != nil {
		slog.Warn("loan grpc: settle disbursement batch failed", "id", req.GetBatchId(), "err", err)
		return &loanv1.SettleBatchResponse{Ok: false}, nil
	}
	return &loanv1.SettleBatchResponse{Ok: true}, nil
}

func (s *LoanServer) ResolveBatch(ctx context.Context, req *loanv1.ResolveBatchRequest) (*loanv1.ResolveBatchResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if req.GetBatchId() == "" || req.GetDecision() == "" {
		return nil, status.Error(codes.InvalidArgument, "batch_id and decision are required")
	}
	if isCollectionBatchType(req.GetBatchType()) {
		if err := s.batchColSvc().Resolve(ctx, tenantID, req.GetBatchId(), req.GetDecision()); err != nil {
			slog.Warn("loan grpc: resolve collection batch failed", "id", req.GetBatchId(), "err", err)
			return &loanv1.ResolveBatchResponse{Ok: false}, nil
		}
		return &loanv1.ResolveBatchResponse{Ok: true}, nil
	}
	if err := s.batchDisbSvc().Resolve(ctx, tenantID, req.GetBatchId(), req.GetDecision()); err != nil {
		slog.Warn("loan grpc: resolve disbursement batch failed", "id", req.GetBatchId(), "err", err)
		return &loanv1.ResolveBatchResponse{Ok: false}, nil
	}
	return &loanv1.ResolveBatchResponse{Ok: true}, nil
}
