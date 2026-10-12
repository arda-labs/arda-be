package grpc

import (
	"context"
	"database/sql"
	"strings"

	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	hrmv1 "github.com/arda-labs/arda/libs/go/arda-proto/hrm/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// KindEmployeeRegister is the shared maker-checker worker kind for employee
// registrations (business_operation_types.worker_kind).
const KindEmployeeRegister = "hrm.employee-register"

// CaseCommandServer implements arda.common.v1.CaseCommandService for the flows
// hrm-service owns, so they run on workflow-service's shared
// common-maker-checker process without HRM-specific workflow workers
// (ADR-011). It delegates to the existing EmployeeCommandService logic.
type CaseCommandServer struct {
	commonv1.UnimplementedCaseCommandServiceServer
	employees *EmployeeServer
}

func NewCaseCommandServer(employees *EmployeeServer) *CaseCommandServer {
	return &CaseCommandServer{employees: employees}
}

func (s *CaseCommandServer) CheckCase(ctx context.Context, req *commonv1.CheckCaseRequest) (*commonv1.CheckCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(req.GetKind()) {
	case KindEmployeeRegister:
		resp, err := s.employees.CheckRegistration(ctx, &hrmv1.CheckRegistrationRequest{RegistrationId: objectID})
		if err != nil {
			return nil, err
		}
		return &commonv1.CheckCaseResponse{Ok: resp.GetOk(), Message: resp.GetMessage()}, nil
	default:
		return nil, unsupportedKind(req.GetKind())
	}
}

func (s *CaseCommandServer) ResolveCase(ctx context.Context, req *commonv1.ResolveCaseRequest) (*commonv1.ResolveCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.GetKind()) != KindEmployeeRegister {
		return nil, unsupportedKind(req.GetKind())
	}
	switch req.GetDecision() {
	case commonv1.CaseDecision_CASE_DECISION_APPROVE:
		// SettleRegistration upserts the employee and accepts an already
		// APPROVED registration, so a retried approval returns the same id.
		resp, err := s.employees.SettleRegistration(ctx, &hrmv1.SettleRegistrationRequest{
			RegistrationId: objectID, ApprovedBy: req.GetDecidedBy(),
		})
		if err != nil {
			return nil, err
		}
		return &commonv1.ResolveCaseResponse{Outputs: map[string]string{"employeeId": resp.GetEmployeeId()}}, nil
	case commonv1.CaseDecision_CASE_DECISION_REJECT:
		resp, err := s.employees.RejectRegistration(ctx, &hrmv1.RejectRegistrationRequest{
			RegistrationId: objectID, DecidedBy: req.GetDecidedBy(), Note: req.GetNote(),
		})
		if err != nil {
			return nil, err
		}
		if !resp.GetOk() {
			if err := s.requireRejected(ctx, objectID); err != nil {
				return nil, err
			}
		}
		return &commonv1.ResolveCaseResponse{}, nil
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported case decision %s", req.GetDecision())
	}
}

// requireRejected makes a retried rejection succeed: RejectRegistration only
// transitions SUBMITTED/IN_REVIEW rows, so a second call after a successful
// rejection updates nothing.
func (s *CaseCommandServer) requireRejected(ctx context.Context, registrationID string) error {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	var current string
	err = s.employees.db.QueryRowContext(ctx,
		`SELECT status FROM hrm_employee_registrations WHERE tenant_id = $1 AND id = $2`,
		tenantID, registrationID).Scan(&current)
	if err == sql.ErrNoRows {
		return status.Errorf(codes.NotFound, "registration %s not found", registrationID)
	}
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if normalizeRegistrationStatus(current) != "REJECTED" {
		return status.Errorf(codes.FailedPrecondition, "registration %s status %s cannot be rejected", registrationID, normalizeRegistrationStatus(current))
	}
	return nil
}

func caseObjectID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", status.Error(codes.InvalidArgument, "object_id is required")
	}
	return id, nil
}

func unsupportedKind(kind string) error {
	return status.Errorf(codes.InvalidArgument, "case kind %q is not served by hrm-service", kind)
}
