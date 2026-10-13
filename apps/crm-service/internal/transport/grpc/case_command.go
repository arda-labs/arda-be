package grpc

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	"github.com/arda-labs/arda/apps/crm-service/internal/service"
	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	crmv1 "github.com/arda-labs/arda/libs/go/arda-proto/crm/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Shared maker-checker worker kinds crm-service owns
// (business_operation_types.worker_kind, ADR-011).
const (
	// KindCustomerRegister is CUSTOMER_REGISTRATION: the customer row itself is
	// the case object; the check is the duplicate-identity rule.
	KindCustomerRegister = "crm.customer-register"
	// KindMemberRequest is CRM_MEMBER_V1: a staged crm_member_requests row
	// (REGISTER, ADDITIONAL or WITHDRAW of membership capital).
	KindMemberRequest = "crm.member-request"
)

// CaseCommandServer implements arda.common.v1.CaseCommandService for the flows
// crm-service owns, so they run on workflow-service's shared
// common-maker-checker process without CRM-specific workflow workers. It
// delegates to the existing CustomerCommandService and MemberCommandService
// logic; the decision semantics are those of the legacy crm.* workers.
type CaseCommandServer struct {
	commonv1.UnimplementedCaseCommandServiceServer
	customers *CustomerCommandServer
	members   *service.MemberService
}

func NewCaseCommandServer(customers *CustomerCommandServer, members *service.MemberService) *CaseCommandServer {
	return &CaseCommandServer{customers: customers, members: members}
}

func (s *CaseCommandServer) CheckCase(ctx context.Context, req *commonv1.CheckCaseRequest) (*commonv1.CheckCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(req.GetKind()) {
	case KindCustomerRegister:
		return s.checkCustomerRegistration(ctx, objectID)
	case KindMemberRequest:
		tenantID, err := memberTenant(ctx)
		if err != nil {
			return nil, err
		}
		ok, message, err := s.members.CheckCapitalRequest(ctx, tenantID, objectID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &commonv1.CheckCaseResponse{Ok: ok, Message: message}, nil
	default:
		return nil, unsupportedKind(req.GetKind())
	}
}

func (s *CaseCommandServer) ResolveCase(ctx context.Context, req *commonv1.ResolveCaseRequest) (*commonv1.ResolveCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	if req.GetDecision() != commonv1.CaseDecision_CASE_DECISION_APPROVE && req.GetDecision() != commonv1.CaseDecision_CASE_DECISION_REJECT {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported case decision %s", req.GetDecision())
	}
	approve := req.GetDecision() == commonv1.CaseDecision_CASE_DECISION_APPROVE
	switch strings.TrimSpace(req.GetKind()) {
	case KindCustomerRegister:
		return s.resolveCustomerRegistration(ctx, objectID, approve)
	case KindMemberRequest:
		return s.resolveMemberRequest(ctx, objectID, approve, req.GetDecidedBy(), req.GetDataVersion())
	default:
		return nil, unsupportedKind(req.GetKind())
	}
}

// checkCustomerRegistration is the legacy crm.customer.register.validate rule:
// a duplicate identity fails the check and parks the customer in
// NEEDS_CHANGES so the maker's screen matches the case state.
func (s *CaseCommandServer) checkCustomerRegistration(ctx context.Context, customerID string) (*commonv1.CheckCaseResponse, error) {
	scope, err := customerScope(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	customer, err := s.customers.customerRepo.GetScoped(ctx, scope, customerID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if customer == nil {
		return &commonv1.CheckCaseResponse{Ok: false, Message: "customer not found"}, nil
	}
	duplicate, err := s.customers.customerRepo.HasDuplicateIdentityScoped(ctx, scope, customerID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !duplicate {
		return &commonv1.CheckCaseResponse{Ok: true}, nil
	}
	if _, err := s.customers.UpdateCustomerStatus(ctx, &crmv1.UpdateCustomerStatusRequest{CustomerId: customerID, Status: "NEEDS_CHANGES"}); err != nil {
		return nil, err
	}
	return &commonv1.CheckCaseResponse{Ok: false, Message: "Trùng định danh khách hàng"}, nil
}

// resolveCustomerRegistration applies the checker decision to the customer
// row. UpdateCustomerStatus is a plain status write, so a repeated decision
// lands on the same state; the opposite decision after a terminal state is
// refused instead of flipping an active customer.
func (s *CaseCommandServer) resolveCustomerRegistration(ctx context.Context, customerID string, approve bool) (*commonv1.ResolveCaseResponse, error) {
	scope, err := customerScope(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	customer, err := s.customers.customerRepo.GetScoped(ctx, scope, customerID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if customer == nil {
		return nil, status.Errorf(codes.NotFound, "customer %s not found", customerID)
	}
	target, terminalOther := "ACTIVE", "REJECTED"
	if !approve {
		target, terminalOther = "REJECTED", "ACTIVE"
	}
	if customer.Status == terminalOther {
		return nil, status.Errorf(codes.FailedPrecondition, "customer %s is already %s", customerID, customer.Status)
	}
	if customer.Status != target {
		if _, err := s.customers.UpdateCustomerStatus(ctx, &crmv1.UpdateCustomerStatusRequest{CustomerId: customerID, Status: target}); err != nil {
			return nil, err
		}
	}
	return &commonv1.ResolveCaseResponse{Outputs: map[string]string{"customerStatus": target}}, nil
}

// resolveMemberRequest applies the checker decision to a staged capital
// request. ResolveCapitalRequest returns the stored row without error when
// the request was already decided, so the stored decision is compared with
// the requested one: the same decision is the idempotent retry, the other
// one is refused.
func (s *CaseCommandServer) resolveMemberRequest(ctx context.Context, requestID string, approve bool, actor, dataVersion string) (*commonv1.ResolveCaseResponse, error) {
	tenantID, err := memberTenant(ctx)
	if err != nil {
		return nil, err
	}
	decision := "REJECTED"
	if approve {
		decision = "APPROVED"
	}
	version, err := parseDataVersion(dataVersion)
	if err != nil {
		return nil, err
	}
	request, _, err := s.members.ResolveCapitalRequest(ctx, tenantID, requestID, decision, actor, version)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrMemberVersionConflict):
			return nil, status.Error(codes.Aborted, err.Error())
		case errors.Is(err, repository.ErrMemberRequestNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		case errors.Is(err, repository.ErrMemberNotActive), errors.Is(err, repository.ErrMemberInsufficientCapital):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if request.Status != decision {
		return nil, status.Errorf(codes.FailedPrecondition, "member request %s is already %s", requestID, request.Status)
	}
	return &commonv1.ResolveCaseResponse{Outputs: map[string]string{"memberRequestStatus": decision}}, nil
}

// parseDataVersion converts the checker's row version (a decimal string from
// the task form) to the int64 the member repository guards with; empty means
// unguarded (0), anything else must be a number.
func parseDataVersion(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version < 0 {
		return 0, status.Errorf(codes.InvalidArgument, "data_version %q is not a row version", raw)
	}
	return version, nil
}

func caseObjectID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", status.Error(codes.InvalidArgument, "object_id is required")
	}
	return id, nil
}

func unsupportedKind(kind string) error {
	return status.Errorf(codes.InvalidArgument, "case kind %q is not served by crm-service", kind)
}
