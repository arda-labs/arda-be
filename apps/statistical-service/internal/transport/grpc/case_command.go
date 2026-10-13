package grpc

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// KindReportSubmit is the shared maker-checker worker kind for RPT_SUBMIT_V2
// (business_operation_types.worker_kind, ADR-011): a staged
// rpt_report_submissions row.
const KindReportSubmit = "rpt.submit"

// CaseCommandServer implements arda.common.v1.CaseCommandService for the
// report submission flow statistical-service owns, so it runs on
// workflow-service's shared common-maker-checker process without the
// rpt.submit.* workers. It delegates to StatisticalService, whose
// ResolveSubmission is already idempotent for a repeated decision.
type CaseCommandServer struct {
	commonv1.UnimplementedCaseCommandServiceServer
	svc *service.StatisticalService
}

func NewCaseCommandServer(svc *service.StatisticalService) *CaseCommandServer {
	return &CaseCommandServer{svc: svc}
}

func (s *CaseCommandServer) CheckCase(ctx context.Context, req *commonv1.CheckCaseRequest) (*commonv1.CheckCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.GetKind()) != KindReportSubmit {
		return nil, unsupportedKind(req.GetKind())
	}
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	ok, message, err := s.svc.CheckSubmission(ctx, tenantID, objectID)
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
	if strings.TrimSpace(req.GetKind()) != KindReportSubmit {
		return nil, unsupportedKind(req.GetKind())
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
	if err := s.svc.ResolveSubmission(ctx, tenantID, objectID, decision, req.GetDecidedBy(), version); err != nil {
		if !s.alreadyDecided(ctx, tenantID, objectID, decision) {
			return nil, resolveError(err)
		}
	}
	return &commonv1.ResolveCaseResponse{Outputs: map[string]string{"submissionDecision": decision}}, nil
}

// alreadyDecided makes a retried decision succeed: the first call bumped the
// row version, so the same checker token now reads as stale, but the row is
// already in the state this decision asks for.
func (s *CaseCommandServer) alreadyDecided(ctx context.Context, tenantID, id, decision string) bool {
	sub, err := s.svc.GetSubmission(ctx, tenantID, id)
	if err != nil || sub == nil {
		return false
	}
	return (decision == "APPROVE" && sub.Status == "APPROVED") || (decision == "REJECT" && sub.Status == "REJECTED")
}

// resolveError maps the domain failure to the gRPC code workflow acts on: a
// stale checker token is Aborted, a decision that cannot apply any more
// (already decided the other way) is FailedPrecondition, a missing row is
// NotFound; everything else is retried as Internal.
func resolveError(err error) error {
	if service.IsStaleVersion(err) {
		return status.Error(codes.Aborted, "statistical: "+err.Error())
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

func unsupportedKind(kind string) error {
	return status.Errorf(codes.InvalidArgument, "case kind %q is not served by statistical-service", kind)
}
