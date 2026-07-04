package grpc

import (
	"context"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	crmv1 "github.com/arda-labs/arda/libs/go/arda-proto/crm/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CustomerCommandServer struct {
	crmv1.UnimplementedCustomerCommandServiceServer
	customerRepo *repository.CustomerRepository
}

func NewCustomerCommandServer(customerRepo *repository.CustomerRepository) *CustomerCommandServer {
	return &CustomerCommandServer{customerRepo: customerRepo}
}

func (s *CustomerCommandServer) UpdateCustomerStatus(ctx context.Context, req *crmv1.UpdateCustomerStatusRequest) (*crmv1.UpdateCustomerStatusResponse, error) {
	if req.GetCustomerId() == "" || req.GetStatus() == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id and status are required")
	}
	if err := s.customerRepo.UpdateStatus(ctx, req.GetCustomerId(), req.GetStatus()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &crmv1.UpdateCustomerStatusResponse{Ok: true}, nil
}

func (s *CustomerCommandServer) CheckDuplicateIdentity(ctx context.Context, req *crmv1.CheckDuplicateIdentityRequest) (*crmv1.CheckDuplicateIdentityResponse, error) {
	if req.GetCustomerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id is required")
	}
	duplicateFound, err := s.customerRepo.HasDuplicateIdentity(ctx, req.GetCustomerId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &crmv1.CheckDuplicateIdentityResponse{DuplicateFound: duplicateFound}, nil
}
