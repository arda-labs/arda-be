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
// callback surface workflow-service workers use for dpm-settle-v2.
type DepositServer struct {
	depositv1.UnimplementedDepositCommandServiceServer
	settlement *service.SettlementService
}

func NewDepositServer(settlement *service.SettlementService) *DepositServer {
	return &DepositServer{settlement: settlement}
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

// ardametadataFromContext is a thin adapter so the server does not import
// ardametadata directly in this file; kept in sync with loan/crm servers.
func ardametadataFromContext(ctx context.Context) string {
	return ardametadata.FromIncoming(ctx).TenantID
}
