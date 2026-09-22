package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/domain"
	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// MemberCaseType is the workflow case type for member capital movements.
const MemberCaseType = "CRM_MEMBER_V1"

// MemberSubmitter is the workflow surface this service needs (same shape the
// customer amendment flow uses).
type MemberSubmitter interface {
	CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error)
	SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error)
}

// MemberService covers QTDND membership and its capital-movement pipeline.
type MemberService struct {
	repo     *repository.MemberRepository
	workflow MemberSubmitter
}

func NewMemberService(repo *repository.MemberRepository, workflow MemberSubmitter) *MemberService {
	return &MemberService{repo: repo, workflow: workflow}
}

// ListMembers returns the paged member register.
func (s *MemberService) ListMembers(ctx context.Context, p repository.ListMembersParams) ([]domain.Member, int, error) {
	if strings.TrimSpace(p.TenantID) == "" {
		return nil, 0, fmt.Errorf("tenant scope is required")
	}
	return s.repo.ListMembers(ctx, p)
}

// GetMember loads one member by id.
func (s *MemberService) GetMember(ctx context.Context, tenantID, id string) (*domain.Member, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("tenant scope is required")
	}
	return s.repo.GetMember(ctx, tenantID, id)
}

// RegisterMemberInput is the create payload.
type RegisterMemberInput struct {
	MemberCode       string
	CustomerCode     string
	OrgCode          string
	MemberBookNo     string
	MemberTypeCode   string
	OpenDate         string
	EstbCapitalMinor int64
	Actor            string
}

// RegisterMember turns a customer into a member. The customer must exist and no
// member may already reference it — membership is the customer's equity stake,
// not a second identity.
func (s *MemberService) RegisterMember(ctx context.Context, tenantID string, in RegisterMemberInput) (*domain.Member, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("tenant scope is required")
	}
	if strings.TrimSpace(in.CustomerCode) == "" {
		return nil, fmt.Errorf("customer_code is required")
	}
	if strings.TrimSpace(in.OpenDate) == "" {
		return nil, fmt.Errorf("open_date is required")
	}
	if in.EstbCapitalMinor < 0 {
		return nil, fmt.Errorf("estb_capital_minor cannot be negative")
	}
	typeCode := in.MemberTypeCode
	if typeCode == "" {
		typeCode = "INDIVIDUAL"
	}
	if !validMemberType(typeCode) {
		return nil, fmt.Errorf("member_type_code must be one of INDIVIDUAL, HOUSEHOLD, LEGAL")
	}
	return s.repo.CreateMember(ctx, &domain.Member{
		TenantID:         tenantID,
		MemberCode:       in.MemberCode,
		CustomerCode:     in.CustomerCode,
		OrgCode:          in.OrgCode,
		MemberBookNo:     in.MemberBookNo,
		MemberTypeCode:   typeCode,
		OpenDate:         in.OpenDate,
		EstbCapitalMinor: in.EstbCapitalMinor,
		MemberStatus:     "ACTIVE",
		CreatedBy:        in.Actor,
	})
}

func validMemberType(code string) bool {
	for _, known := range []string{"INDIVIDUAL", "HOUSEHOLD", "LEGAL"} {
		if code == known {
			return true
		}
	}
	return false
}

// UpdateMemberProfile updates the descriptive fields with a version guard.
func (s *MemberService) UpdateMemberProfile(ctx context.Context, tenantID, id, bookNo, typeCode, status, actor string, version int64) (*domain.Member, error) {
	if !validMemberType(typeCode) {
		return nil, fmt.Errorf("member_type_code must be one of INDIVIDUAL, HOUSEHOLD, LEGAL")
	}
	if status != "ACTIVE" && status != "LEFT" {
		return nil, fmt.Errorf("member_status must be ACTIVE or LEFT")
	}
	return s.repo.UpdateMemberProfile(ctx, tenantID, id, bookNo, typeCode, status, actor, version)
}

// SubmitCapitalRequestInput stages one capital movement and pushes it to the
// CRM_MEMBER_V1 case.
type SubmitCapitalRequestInput struct {
	MemberID       string
	RequestType    string
	ProductCode    string
	AmountMinor    int64
	CurrencyCode   string
	EffectiveDate  string
	Reason         string
	IdempotencyKey string
	Actor          string
}

// SubmitCapitalRequest validates, persists the DRAFT request and submits the
// maker-checker case. The guard runs before the case so a request that can
// never be approved (e.g. a withdrawal beyond the stake) never enters the
// approval queue.
func (s *MemberService) SubmitCapitalRequest(ctx context.Context, tenantID string, in SubmitCapitalRequestInput) (*domain.MemberRequest, error) {
	if !validRequestType(in.RequestType) {
		return nil, fmt.Errorf("request_type must be one of %s", strings.Join(domain.MemberRequestTypes, ", "))
	}
	if in.AmountMinor <= 0 {
		return nil, fmt.Errorf("amount_minor must be positive")
	}
	member, err := s.repo.GetMember(ctx, tenantID, in.MemberID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, fmt.Errorf("member not found")
	}
	if member.MemberStatus != "ACTIVE" {
		return nil, fmt.Errorf("member is not active")
	}
	if in.RequestType == domain.MemberRequestWithdraw && in.AmountMinor > member.TotalCapitalMinor {
		return nil, fmt.Errorf("withdrawal %d exceeds available capital %d", in.AmountMinor, member.TotalCapitalMinor)
	}
	currency := in.CurrencyCode
	if currency == "" {
		currency = "VND"
	}

	req := &domain.MemberRequest{
		TenantID:       tenantID,
		MemberID:       in.MemberID,
		RequestType:    in.RequestType,
		ProductCode:    in.ProductCode,
		AmountMinor:    in.AmountMinor,
		CurrencyCode:   currency,
		EffectiveDate:  in.EffectiveDate,
		Status:         "DRAFT",
		Reason:         in.Reason,
		IdempotencyKey: in.IdempotencyKey,
		CreatedBy:      in.Actor,
	}
	created, err := s.repo.CreateMemberRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	if s.workflow == nil {
		return created, nil
	}
	cs, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		CaseType:  MemberCaseType,
		Title:     fmt.Sprintf("Vốn góp thành viên %s — %s", member.MemberCode, in.RequestType),
		CreatedBy: in.Actor,
	})
	if err != nil {
		return created, fmt.Errorf("create member case: %w", err)
	}
	if _, err := s.workflow.SubmitCase(ctx, cs.Id, in.Actor, map[string]any{
		"request_id":   created.ID,
		"member_code":  member.MemberCode,
		"request_type": in.RequestType,
		"amount_minor": in.AmountMinor,
		"org_code":     member.OrgCode,
	}, in.IdempotencyKey); err != nil {
		return created, fmt.Errorf("submit member case: %w", err)
	}
	if _, err := s.repo.MarkMemberRequestSubmitted(ctx, tenantID, created.ID, cs.Id, in.Actor); err != nil {
		return created, err
	}
	created.Status = "SUBMITTED"
	created.WorkflowCaseID = &cs.Id
	return created, nil
}

func validRequestType(t string) bool {
	for _, known := range domain.MemberRequestTypes {
		if t == known {
			return true
		}
	}
	return false
}

// ListMemberRequests returns the request pipeline.
func (s *MemberService) ListMemberRequests(ctx context.Context, tenantID, memberID, status string) ([]domain.MemberRequest, error) {
	return s.repo.ListMemberRequests(ctx, tenantID, memberID, status)
}

// ListMembersForReporting is the statistical ETL read of the member register.
func (s *MemberService) ListMembersForReporting(ctx context.Context, tenantID, orgCode string) ([]domain.Member, error) {
	return s.repo.ListMembersForReporting(ctx, tenantID, orgCode)
}

// ListMemberRequestsForReporting is the statistical ETL read of the capital
// request pipeline.
func (s *MemberService) ListMemberRequestsForReporting(ctx context.Context, tenantID string) ([]repository.MemberCapitalRequestRow, error) {
	return s.repo.ListMemberRequestsForReporting(ctx, tenantID)
}

// CheckCapitalRequest re-reads the staged request and reports whether the
// checker may approve it. This is the validate worker's callback: a request
// that can never be approved (inactive member, withdrawal beyond the stake)
// must fail validation, not reach the checker.
func (s *MemberService) CheckCapitalRequest(ctx context.Context, tenantID, requestID string) (bool, string, error) {
	req, err := s.repo.GetMemberRequest(ctx, tenantID, requestID)
	if err != nil {
		return false, "", err
	}
	if req == nil {
		return false, "yêu cầu không tồn tại", nil
	}
	if req.Status == "APPROVED" || req.Status == "REJECTED" {
		return false, "yêu cầu đã được xử lý", nil
	}
	member, err := s.repo.GetMember(ctx, tenantID, req.MemberID)
	if err != nil {
		return false, "", err
	}
	if member == nil {
		return false, "thành viên không tồn tại", nil
	}
	if member.MemberStatus != "ACTIVE" {
		return false, "thành viên không còn hoạt động", nil
	}
	if req.RequestType == domain.MemberRequestWithdraw && req.AmountMinor > member.TotalCapitalMinor {
		return false, "số tiền rút vượt quá vốn góp hiện có", nil
	}
	return true, "", nil
}

// ResolveCapitalRequest is the checker decision. Approving moves the member's
// capital and marks the request APPROVED in one transaction; rejecting only
// closes the request. The member row is locked FOR UPDATE so two approvals
// cannot both apply.
func (s *MemberService) ResolveCapitalRequest(ctx context.Context, tenantID, id, decision, actor string, dataVersion int64) (*domain.MemberRequest, *domain.Member, error) {
	if decision != "APPROVED" && decision != "REJECTED" {
		return nil, nil, fmt.Errorf("decision must be APPROVED or REJECTED")
	}
	req, err := s.repo.GetMemberRequest(ctx, tenantID, id)
	if err != nil {
		return nil, nil, err
	}
	if req == nil {
		return nil, nil, fmt.Errorf("member request not found")
	}
	if req.Status != "SUBMITTED" {
		return req, nil, nil // idempotent: already decided
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ok, err := s.repo.ResolveMemberRequest(ctx, tx, tenantID, id, decision, actor, dataVersion)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, repository.ErrMemberVersionConflict
	}

	var member *domain.Member
	if decision == "APPROVED" {
		member, err = s.repo.ApplyApprovedCapital(ctx, tx, tenantID, req.MemberID, req.RequestType, req.AmountMinor, actor)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	req.Status = decision
	req.DecidedBy = actor
	return req, member, nil
}
