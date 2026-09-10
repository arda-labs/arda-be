package grpc

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	depositv1 "github.com/arda-labs/arda/libs/go/arda-proto/deposit/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DepositServer implements arda.deposit.v1.DepositCommandService — the
// callback surface workflow-service workers use for dpm-settle-v2 and
// dpm-additional-v1.
type DepositServer struct {
	depositv1.UnimplementedDepositCommandServiceServer
	settlement *service.SettlementService
	additional *service.AdditionalDepositService
	products   *service.ProductRequestService
	ibm        *service.IBMService
	interest   *service.InterestService
}

func NewDepositServer(settlement *service.SettlementService, additional *service.AdditionalDepositService, products *service.ProductRequestService, ibm *service.IBMService, interest *service.InterestService) *DepositServer {
	return &DepositServer{settlement: settlement, additional: additional, products: products, ibm: ibm, interest: interest}
}

func tenantFromContext(ctx context.Context) (string, error) {
	md := ardametadataFromContext(ctx)
	if md == "" {
		return "", fmt.Errorf("tenant scope is required")
	}
	return md, nil
}

func (s *DepositServer) CheckSettle(ctx context.Context, req *depositv1.CheckSettleRequest) (*depositv1.CheckSettleResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	savings, err := s.settlement.GetSavingsByCode(ctx, tenantID, req.GetSavingsCode())
	if err != nil {
		return &depositv1.CheckSettleResponse{Ok: false, Message: "savings not found"}, nil
	}
	if savings.Status != "ACTIVE" {
		return &depositv1.CheckSettleResponse{Ok: false, Message: "status " + savings.Status + " is not actionable"}, nil
	}
	return &depositv1.CheckSettleResponse{Ok: true}, nil
}

func (s *DepositServer) Settle(ctx context.Context, req *depositv1.SettleRequest) (*depositv1.SettleResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	_, err = s.settlement.Settle(ctx, tenantID, req.GetSavingsCode(), req.GetActor())
	if err != nil {
		return &depositv1.SettleResponse{Ok: false}, nil
	}
	return &depositv1.SettleResponse{Ok: true}, nil
}

func (s *DepositServer) CheckAdditional(ctx context.Context, req *depositv1.CheckAdditionalRequest) (*depositv1.CheckAdditionalResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.additional.Check(ctx, tenantID, req.GetSavingsCode(), req.GetAmountMinor())
	if err != nil {
		return &depositv1.CheckAdditionalResponse{Ok: false, Message: err.Error()}, nil
	}
	return &depositv1.CheckAdditionalResponse{Ok: ok, Message: message}, nil
}

func (s *DepositServer) SettleAdditional(ctx context.Context, req *depositv1.SettleAdditionalRequest) (*depositv1.SettleAdditionalResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.additional.Settle(ctx, tenantID, req.GetActor(), req.GetSavingsCode(),
		req.GetAmountMinor(), req.GetTxnDate(), req.GetIdempotencyKey()); err != nil {
		return &depositv1.SettleAdditionalResponse{Ok: false}, nil
	}
	return &depositv1.SettleAdditionalResponse{Ok: true}, nil
}

func (s *DepositServer) CheckProductRequest(ctx context.Context, req *depositv1.CheckProductRequestRequest) (*depositv1.CheckProductRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.products.Check(ctx, tenantID, req.GetProductRequestId())
	if err != nil {
		return &depositv1.CheckProductRequestResponse{Ok: false, Message: err.Error()}, nil
	}
	return &depositv1.CheckProductRequestResponse{Ok: ok, Message: message}, nil
}

func (s *DepositServer) ResolveProductRequest(ctx context.Context, req *depositv1.ResolveProductRequestRequest) (*depositv1.ResolveProductRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.products.Resolve(ctx, tenantID, req.GetProductRequestId(), req.GetDecision(), req.GetActor()); err != nil {
		return &depositv1.ResolveProductRequestResponse{Ok: false}, nil
	}
	return &depositv1.ResolveProductRequestResponse{Ok: true}, nil
}

func (s *DepositServer) CheckIBMRequest(ctx context.Context, req *depositv1.CheckIBMRequestRequest) (*depositv1.CheckIBMRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.ibm.CheckIBMRequest(ctx, tenantID, req.GetKind(), req.GetRefId())
	if err != nil {
		return &depositv1.CheckIBMRequestResponse{Ok: false, Message: err.Error()}, nil
	}
	return &depositv1.CheckIBMRequestResponse{Ok: ok, Message: message}, nil
}

func (s *DepositServer) ResolveIBMRequest(ctx context.Context, req *depositv1.ResolveIBMRequestRequest) (*depositv1.ResolveIBMRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.ibm.ResolveIBMRequest(ctx, tenantID, req.GetKind(), req.GetRefId(), req.GetDecision(), req.GetActor()); err != nil {
		return &depositv1.ResolveIBMRequestResponse{Ok: false}, nil
	}
	return &depositv1.ResolveIBMRequestResponse{Ok: true}, nil
}

func (s *DepositServer) CheckRateRequest(ctx context.Context, req *depositv1.CheckRateRequestRequest) (*depositv1.CheckRateRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.interest.CheckRateRequest(ctx, tenantID, req.GetRequestId())
	if err != nil {
		return &depositv1.CheckRateRequestResponse{Ok: false, Message: err.Error()}, nil
	}
	return &depositv1.CheckRateRequestResponse{Ok: ok, Message: message}, nil
}

func (s *DepositServer) ResolveRateRequest(ctx context.Context, req *depositv1.ResolveRateRequestRequest) (*depositv1.ResolveRateRequestResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.interest.ResolveRateRequest(ctx, tenantID, req.GetRequestId(), req.GetDecision(), req.GetActor()); err != nil {
		return &depositv1.ResolveRateRequestResponse{Ok: false}, nil
	}
	return &depositv1.ResolveRateRequestResponse{Ok: true}, nil
}

func (s *DepositServer) CheckInterestOp(ctx context.Context, req *depositv1.CheckInterestOpRequest) (*depositv1.CheckInterestOpResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	ok, message, err := s.interest.CheckInterestOp(ctx, tenantID, req.GetOpId())
	if err != nil {
		return &depositv1.CheckInterestOpResponse{Ok: false, Message: err.Error()}, nil
	}
	return &depositv1.CheckInterestOpResponse{Ok: ok, Message: message}, nil
}

func (s *DepositServer) ResolveInterestOp(ctx context.Context, req *depositv1.ResolveInterestOpRequest) (*depositv1.ResolveInterestOpResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if err := s.interest.ResolveInterestOp(ctx, tenantID, req.GetOpId(), req.GetDecision(), req.GetActor()); err != nil {
		return &depositv1.ResolveInterestOpResponse{Ok: false}, nil
	}
	return &depositv1.ResolveInterestOpResponse{Ok: true}, nil
}

// ardametadataFromContext is a thin adapter so the server does not import
// ardametadata directly in this file; kept in sync with loan/crm servers.
func ardametadataFromContext(ctx context.Context) string {
	return ardametadata.FromIncoming(ctx).TenantID
}
