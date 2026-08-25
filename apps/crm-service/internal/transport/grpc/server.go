package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	crmv1 "github.com/arda-labs/arda/libs/go/arda-proto/crm/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CustomerCommandServer struct {
	crmv1.UnimplementedCustomerCommandServiceServer
	customerRepo  *repository.CustomerRepository
	amendmentRepo *repository.AmendmentRepository
}

func NewCustomerCommandServer(customerRepo *repository.CustomerRepository, amendmentRepo *repository.AmendmentRepository) *CustomerCommandServer {
	return &CustomerCommandServer{
		customerRepo:  customerRepo,
		amendmentRepo: amendmentRepo,
	}
}

func (s *CustomerCommandServer) UpdateCustomerStatus(ctx context.Context, req *crmv1.UpdateCustomerStatusRequest) (*crmv1.UpdateCustomerStatusResponse, error) {
	if req.GetCustomerId() == "" || req.GetStatus() == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id and status are required")
	}
	scope, err := customerScope(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if customer, err := s.customerRepo.GetScoped(ctx, scope, req.GetCustomerId()); err != nil || customer == nil {
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return nil, status.Error(codes.NotFound, "customer not found")
	}
	slog.Info("crm workflow status update requested",
		"customerId", req.GetCustomerId(),
		"status", req.GetStatus(),
	)

	if s.amendmentRepo != nil {
		pending, err := s.amendmentRepo.GetPendingForWorkflowScoped(ctx, scope, req.GetCustomerId())
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if pending != nil {
			switch req.GetStatus() {
			case "APPROVED", "ACTIVE":
				if err := s.amendmentRepo.ApplyScoped(ctx, scope, req.GetCustomerId(), "workflow"); err != nil {
					return nil, status.Error(codes.Internal, err.Error())
				}
				return &crmv1.UpdateCustomerStatusResponse{Ok: true}, nil
			case "REJECTED":
				if err := s.amendmentRepo.DiscardScoped(ctx, scope, req.GetCustomerId(), "workflow"); err != nil {
					return nil, status.Error(codes.Internal, err.Error())
				}
				return &crmv1.UpdateCustomerStatusResponse{Ok: true}, nil
			case "NEEDS_CHANGES":
				if err := s.amendmentRepo.ReopenPendingScoped(ctx, scope, req.GetCustomerId()); err != nil {
					return nil, status.Error(codes.Internal, err.Error())
				}
				return &crmv1.UpdateCustomerStatusResponse{Ok: true}, nil
			}
		}
	}

	nextStatus := req.GetStatus()
	if nextStatus == "APPROVED" {
		nextStatus = "ACTIVE"
	}
	if err := s.customerRepo.UpdateStatusScoped(ctx, scope, req.GetCustomerId(), nextStatus); err != nil {
		slog.Error("crm workflow status update failed",
			"customerId", req.GetCustomerId(),
			"status", nextStatus,
			"err", err,
		)
		return nil, status.Error(codes.Internal, err.Error())
	}
	if nextStatus == "ACTIVE" {
		if err := s.customerRepo.AssignOfficialCustomerCodeScoped(ctx, scope, req.GetCustomerId()); err != nil {
			current, getErr := s.customerRepo.GetScoped(ctx, scope, req.GetCustomerId())
			if getErr == nil && current != nil && current.Status == "ACTIVE" {
				slog.Warn("crm assign official customer code skipped after active",
					"customerId", req.GetCustomerId(),
					"customerCode", current.CustomerCode,
					"err", err,
				)
			} else {
				slog.Error("crm assign official customer code failed",
					"customerId", req.GetCustomerId(),
					"err", err,
				)
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	slog.Info("crm workflow status update succeeded",
		"customerId", req.GetCustomerId(),
		"status", nextStatus,
	)
	return &crmv1.UpdateCustomerStatusResponse{Ok: true}, nil
}

func (s *CustomerCommandServer) CheckDuplicateIdentity(ctx context.Context, req *crmv1.CheckDuplicateIdentityRequest) (*crmv1.CheckDuplicateIdentityResponse, error) {
	if req.GetCustomerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id is required")
	}
	scope, err := customerScope(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	duplicateFound, err := s.customerRepo.HasDuplicateIdentityScoped(ctx, scope, req.GetCustomerId())
	if err != nil {
		slog.Error("crm duplicate check failed",
			"customerId", req.GetCustomerId(),
			"err", err,
		)
		return nil, status.Error(codes.Internal, err.Error())
	}
	slog.Info("crm duplicate check completed",
		"customerId", req.GetCustomerId(),
		"duplicateFound", duplicateFound,
	)
	return &crmv1.CheckDuplicateIdentityResponse{DuplicateFound: duplicateFound}, nil
}

func customerScope(ctx context.Context) (repository.CustomerScope, error) {
	md := ardametadata.FromIncoming(ctx)
	tenantID := strings.TrimSpace(md.TenantID)
	orgIDs := append([]string(nil), md.OrgIDs...)
	if md.OrgID != "" && !contains(orgIDs, md.OrgID) {
		orgIDs = append(orgIDs, md.OrgID)
	}
	if tenantID == "" || len(orgIDs) == 0 {
		return repository.CustomerScope{}, fmt.Errorf("tenant and organization scope are required")
	}
	return repository.CustomerScope{TenantID: tenantID, OrgIDs: orgIDs}, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
