package grpc

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Shared maker-checker worker kinds deposit-service owns
// (business_operation_types.worker_kind, ADR-011).
const (
	// KindSettle is DPM_SETTLE_V2; the object id is the savings code.
	KindSettle = "dpm.settle"
	// KindProductRequest is DPM_PRODUCT_REGISTER_V1 and DPM_PRODUCT_EDIT_V1; the
	// object id is the staged dpm_product_requests row.
	KindProductRequest = "dpm.product-request"
	// KindRateRequest is DPM_RATE_REGISTER_V1 and DPM_RATE_EDIT_V1; the object
	// id is the staged dpm_rate_requests row.
	KindRateRequest = "dpm.rate-request"
	// KindInterestOp is DPM_PAY_INTEREST_V1 and DPM_CAPITALIZE_V1; the object
	// id is one staged dpm_interest_ops row.
	KindInterestOp = "dpm.interest-op"
	// KindInterestBatch is DPM_BATCH_INTEREST_V1: every dpm_interest_ops row
	// stamped with the case id (the object id is only the first op).
	KindInterestBatch = "dpm.interest-batch"
	// IBM kinds map onto the staged object kinds IBMService resolves.
	KindIBMPlace    = "ibm.place"    // IBM_PLACE_V1: ibm_deposits PENDING_APPROVAL -> ACTIVE (posts)
	KindIBMBorrow   = "ibm.borrow"   // IBM_BORROW_V1: ibm_borrows PENDING_APPROVAL -> ACTIVE
	KindIBMTopUp    = "ibm.top-up"   // IBM_TOP_UP_V1: ibm_movements SUBMITTED -> POSTED (posts)
	KindIBMInterest = "ibm.interest" // IBM_INTEREST_V1
	KindIBMExpected = "ibm.expected" // IBM_EXPECTED_V1
	KindIBMWithdraw = "ibm.withdraw" // IBM_WITHDRAW_V1
)

var ibmKinds = map[string]string{
	KindIBMPlace:    service.IBMKindPlace,
	KindIBMBorrow:   service.IBMKindBorrow,
	KindIBMTopUp:    service.IBMKindTopUp,
	KindIBMInterest: service.IBMKindInterest,
	KindIBMExpected: service.IBMKindExpected,
	KindIBMWithdraw: service.IBMKindWithdraw,
}

// CaseCommandServer implements arda.common.v1.CaseCommandService for the flows
// deposit-service owns, so they run on workflow-service's shared
// common-maker-checker process without the dpm.* and ibm.* workers. Every
// side effect of an approval, including GL posting through finance, stays in
// the domain services this adapter delegates to; the adapter adds the
// idempotency the shared contract requires where a service lacks it.
type CaseCommandServer struct {
	commonv1.UnimplementedCaseCommandServiceServer
	settlement *service.SettlementService
	products   *service.ProductRequestService
	ibm        *service.IBMService
	interest   *service.InterestService
}

func NewCaseCommandServer(settlement *service.SettlementService, products *service.ProductRequestService, ibm *service.IBMService, interest *service.InterestService) *CaseCommandServer {
	return &CaseCommandServer{settlement: settlement, products: products, ibm: ibm, interest: interest}
}

func (s *CaseCommandServer) CheckCase(ctx context.Context, req *commonv1.CheckCaseRequest) (*commonv1.CheckCaseResponse, error) {
	objectID, err := caseObjectID(req.GetObjectId())
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(req.GetKind())
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	var ok bool
	var message string
	switch kind {
	case KindSettle:
		ok, message, err = s.checkSettle(ctx, tenantID, objectID)
	case KindProductRequest:
		ok, message, err = s.products.Check(ctx, tenantID, objectID)
	case KindRateRequest:
		ok, message, err = s.interest.CheckRateRequest(ctx, tenantID, objectID)
	case KindInterestOp:
		ok, message, err = s.interest.CheckInterestOp(ctx, tenantID, objectID)
	case KindInterestBatch:
		ok, message, err = s.checkInterestBatch(ctx, tenantID, req.GetCaseId())
	default:
		ibmKind, known := ibmKinds[kind]
		if !known {
			return nil, unsupportedKind(kind)
		}
		ok, message, err = s.ibm.CheckIBMRequest(ctx, tenantID, ibmKind, objectID)
	}
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
	var decision string
	switch req.GetDecision() {
	case commonv1.CaseDecision_CASE_DECISION_APPROVE:
		decision = "APPROVE"
	case commonv1.CaseDecision_CASE_DECISION_REJECT:
		decision = "REJECT"
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported case decision %s", req.GetDecision())
	}
	kind := strings.TrimSpace(req.GetKind())
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	actor := req.GetDecidedBy()
	dataVersion := strings.TrimSpace(req.GetDataVersion())
	switch kind {
	case KindSettle:
		err = s.resolveSettle(ctx, tenantID, objectID, decision, actor, dataVersion)
	case KindProductRequest:
		err = s.resolveProductRequest(ctx, tenantID, objectID, decision, actor, dataVersion)
	case KindRateRequest:
		var version int64
		if version, err = parseDataVersion(dataVersion); err == nil {
			err = s.interest.ResolveRateRequest(ctx, tenantID, objectID, decision, actor, version)
		}
	case KindInterestOp:
		err = s.interest.ResolveInterestOp(ctx, tenantID, objectID, decision, actor, dataVersion)
	case KindInterestBatch:
		err = s.resolveInterestBatch(ctx, tenantID, req.GetCaseId(), decision, actor, dataVersion)
	default:
		ibmKind, known := ibmKinds[kind]
		if !known {
			return nil, unsupportedKind(kind)
		}
		err = s.ibm.ResolveIBMRequest(ctx, tenantID, ibmKind, objectID, decision, actor, dataVersion)
	}
	if err != nil {
		return nil, resolveError(err)
	}
	return &commonv1.ResolveCaseResponse{Outputs: map[string]string{"depositDecision": decision}}, nil
}

// checkSettle is the legacy dpm.settle.validate rule: the savings must exist
// in the tenant and be ACTIVE.
func (s *CaseCommandServer) checkSettle(ctx context.Context, tenantID, savingsCode string) (bool, string, error) {
	savings, err := s.settlement.GetSavingsByCode(ctx, tenantID, savingsCode)
	if err != nil || savings == nil {
		return false, "savings not found", nil
	}
	if savings.Status != "ACTIVE" {
		return false, "status " + savings.Status + " is not actionable", nil
	}
	return true, "", nil
}

// resolveSettle closes the savings on APPROVE. Settle refuses a savings that
// is no longer ACTIVE, so a retried approval re-reads the row and accepts a
// CLOSED one. REJECT keeps the legacy no-op, but refuses a savings the
// workflow already settled.
func (s *CaseCommandServer) resolveSettle(ctx context.Context, tenantID, savingsCode, decision, actor, dataVersion string) error {
	if decision == "APPROVE" {
		_, err := s.settlement.Settle(ctx, tenantID, savingsCode, actor, dataVersion)
		if err == nil {
			return nil
		}
		if current, readErr := s.settlement.GetSavingsByCode(ctx, tenantID, savingsCode); readErr == nil && current != nil && current.Status == "CLOSED" {
			return nil
		}
		return err
	}
	current, err := s.settlement.GetSavingsByCode(ctx, tenantID, savingsCode)
	if err != nil || current == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "savings not found: "+savingsCode)
	}
	if current.Status == "CLOSED" {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "savings "+savingsCode+" is already CLOSED")
	}
	return nil
}

// resolveProductRequest applies the decision to a staged product request.
// Resolve refuses a request that is not SUBMITTED, so a retry re-reads the
// row and accepts one already in the state this decision produces.
func (s *CaseCommandServer) resolveProductRequest(ctx context.Context, tenantID, requestID, decision, actor, dataVersion string) error {
	version, err := parseDataVersion(dataVersion)
	if err != nil {
		return err
	}
	resolveErr := s.products.Resolve(ctx, tenantID, requestID, decision, actor, version)
	if resolveErr == nil {
		return nil
	}
	target := "REJECTED"
	if decision == "APPROVE" {
		target = "APPLIED"
	}
	if current, readErr := s.products.Get(ctx, tenantID, requestID); readErr == nil && current != nil && current.Status == target {
		return nil
	}
	return resolveErr
}

// checkInterestBatch validates every staged op of the batch case. The batch
// case's primary object is only the first op, so the ops are read by case id.
func (s *CaseCommandServer) checkInterestBatch(ctx context.Context, tenantID, caseID string) (bool, string, error) {
	ops, err := s.interest.GetInterestOpsByCase(ctx, tenantID, caseID)
	if err != nil {
		return false, "", err
	}
	if len(ops) == 0 {
		return false, "no interest ops are staged for this case", nil
	}
	for i := range ops {
		ok, message, err := s.interest.CheckInterestOp(ctx, tenantID, ops[i].ID)
		if err != nil {
			return false, "", err
		}
		if !ok {
			return false, ops[i].SavingsCode + ": " + message, nil
		}
	}
	return true, "", nil
}

// resolveInterestBatch applies the decision to every op of the batch case.
// ResolveInterestOp is idempotent per op (POSTED and REJECTED are no-ops), so
// a retry after a partial failure resumes with the ops still pending.
func (s *CaseCommandServer) resolveInterestBatch(ctx context.Context, tenantID, caseID, decision, actor, dataVersion string) error {
	ops, err := s.interest.GetInterestOpsByCase(ctx, tenantID, caseID)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return ardaerrors.New(ardaerrors.CodeNotFound, "no interest ops are staged for case "+caseID)
	}
	for i := range ops {
		if err := s.interest.ResolveInterestOp(ctx, tenantID, ops[i].ID, decision, actor, dataVersion); err != nil {
			return err
		}
	}
	return nil
}

// resolveError maps a domain failure to the gRPC code workflow acts on: a
// stale checker token or a row that moved on is Aborted, a decision that
// cannot apply any more (already decided the other way) is
// FailedPrecondition, a missing row is NotFound; everything else, including
// a finance posting outage, is retried as Internal.
func resolveError(err error) error {
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	var appErr *ardaerrors.Error
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case ardaerrors.CodeNotFound:
			return status.Error(codes.NotFound, appErr.Message)
		case ardaerrors.CodeInvalidInput:
			return status.Error(codes.FailedPrecondition, appErr.Message)
		case ardaerrors.CodeConflict:
			return status.Error(codes.Aborted, appErr.Message)
		}
	}
	return status.Error(codes.Internal, err.Error())
}

// parseDataVersion converts the checker's row version (a decimal string from
// the task form) to the int64 some deposit repositories guard with; empty
// means unguarded (0).
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
	return status.Errorf(codes.InvalidArgument, "case kind %q is not served by deposit-service", kind)
}
