package grpc

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/capital-service/internal/domain"
	"github.com/arda-labs/arda/apps/capital-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Shared maker-checker worker kinds capital-service owns
// (business_operation_types.worker_kind, ADR-011). Each maps to the staged
// object kind CapitalService already resolves.
const (
	KindContract  = "cfc.contract"  // CFC_CONTRACT_V1: cfc_contracts PENDING_APPROVAL -> ACTIVE
	KindAmendment = "cfc.amendment" // CFC_AMENDMENT_V1: cfc_contract_amendments SUBMITTED -> APPLIED
	KindMovement  = "cfc.movement"  // CFC_MOVEMENT_V1: cfc_movements SUBMITTED -> POSTED (posts to finance)
)

var requestKinds = map[string]string{
	KindContract:  service.KindContractFormation,
	KindAmendment: service.KindContractAmendment,
	KindMovement:  service.KindMovement,
}

// CaseCommandServer implements arda.common.v1.CaseCommandService for the flows
// capital-service owns, so they run on workflow-service's shared
// common-maker-checker process without the cfc.* workers. CapitalService
// already treats a repeated decision as a no-op and refuses the opposite
// one, so this adapter only maps kinds, the data version and error codes.
type CaseCommandServer struct {
	commonv1.UnimplementedCaseCommandServiceServer
	svc *service.CapitalService
}

func NewCaseCommandServer(svc *service.CapitalService) *CaseCommandServer {
	return &CaseCommandServer{svc: svc}
}

func (s *CaseCommandServer) CheckCase(ctx context.Context, req *commonv1.CheckCaseRequest) (*commonv1.CheckCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	kind, err := requestKind(req.GetKind())
	if err != nil {
		return nil, err
	}
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	ok, message, err := s.svc.CheckRequest(ctx, tenantID, kind, objectID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &commonv1.CheckCaseResponse{Ok: ok, Message: message}, nil
}

func (s *CaseCommandServer) ResolveCase(ctx context.Context, req *commonv1.ResolveCaseRequest) (*commonv1.ResolveCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	kind, err := requestKind(req.GetKind())
	if err != nil {
		return nil, err
	}
	var decision string
	switch req.GetDecision() {
	case commonv1.CaseDecision_CASE_DECISION_APPROVE:
		decision = "APPROVE"
	case commonv1.CaseDecision_CASE_DECISION_REJECT:
		decision = "REJECT"
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported case decision %s", req.GetDecision())
	}
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	version, err := parseDataVersion(req.GetDataVersion())
	if err != nil {
		return nil, err
	}
	ctx = domain.WithDataVersion(ctx, version)
	if err := s.svc.ResolveRequest(ctx, tenantID, kind, objectID, decision, req.GetDecidedBy()); err != nil {
		return nil, resolveError(err)
	}
	return &commonv1.ResolveCaseResponse{Outputs: map[string]string{"capitalDecision": decision}}, nil
}

func requestKind(raw string) (string, error) {
	kind, ok := requestKinds[strings.TrimSpace(raw)]
	if !ok {
		return "", status.Errorf(codes.InvalidArgument, "case kind %q is not served by capital-service", raw)
	}
	return kind, nil
}

// resolveError maps the domain failure to the gRPC code workflow acts on: a
// stale checker token is Aborted, a decision that cannot apply any more
// (already decided the other way, insufficient balance) is
// FailedPrecondition, a missing row is NotFound; everything else is retried
// as Internal.
func resolveError(err error) error {
	if service.IsStaleVersion(err) {
		return status.Error(codes.Aborted, "capital: "+err.Error())
	}
	var appErr *ardaerrors.Error
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case ardaerrors.CodeNotFound:
			return status.Error(codes.NotFound, appErr.Message)
		case ardaerrors.CodeInvalidInput:
			return status.Error(codes.FailedPrecondition, appErr.Message)
		}
	}
	return status.Error(codes.Internal, err.Error())
}

// parseDataVersion converts the checker's row version (a decimal string from
// the task form) to the int64 the repository guards with; empty means
// unguarded (0).
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
