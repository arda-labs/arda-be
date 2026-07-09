package service

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

type WorkflowCommandService struct {
	caseRepo *repository.CaseRepository
	zeebeSvc *ZeebeService
}

type SubmitCaseInput struct {
	Actor     string
	Variables map[string]any
}

func NewWorkflowCommandService(caseRepo *repository.CaseRepository, zeebeSvc *ZeebeService) *WorkflowCommandService {
	return &WorkflowCommandService{caseRepo: caseRepo, zeebeSvc: zeebeSvc}
}

func (s *WorkflowCommandService) CreateCase(ctx context.Context, in repository.CaseCreate) (*repository.BusinessCase, error) {
	return s.caseRepo.CreateCase(ctx, in)
}

func (s *WorkflowCommandService) SubmitCase(ctx context.Context, id string, in SubmitCaseInput) (*repository.BusinessCase, error) {
	bc, err := s.caseRepo.GetCase(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("query case: %w", err)
	}
	if bc == nil {
		return nil, repository.ErrNotFound
	}
	if bc.BpmnProcessID == nil {
		return nil, fmt.Errorf("case has no BPMN process configured")
	}
	if bc.Status != repository.CaseStatusDraft {
		return nil, fmt.Errorf("case status must be %s", repository.CaseStatusDraft)
	}
	if s.zeebeSvc == nil {
		return nil, fmt.Errorf("zeebe service is not configured")
	}
	if in.Actor == "" {
		in.Actor = bc.CreatedBy
	}

	variables := map[string]any{
		"caseId":            bc.ID,
		"caseType":          bc.CaseType,
		"caseCode":          bc.CaseCode,
		"tenantId":          bc.TenantID,
		"domainService":     bc.DomainService,
		"primaryObjectType": bc.PrimaryObjectType,
		"primaryObjectId":   bc.PrimaryObjectID,
	}
	if bc.PrimaryObjectType == "CUSTOMER" {
		variables["customerId"] = bc.PrimaryObjectID
	}
	for key, value := range in.Variables {
		variables[key] = value
	}
	processKey, err := s.zeebeSvc.StartWorkflow(ctx, *bc.BpmnProcessID, variables)
	if err != nil {
		return nil, fmt.Errorf("start workflow: %w", err)
	}
	updated, err := s.caseRepo.SubmitCase(ctx, id, in.Actor, processKey)
	if err != nil {
		return nil, fmt.Errorf("workflow started but case submit failed: %w", err)
	}
	if task, ok := InitialUserTaskForCaseType(updated.CaseType); ok {
		SeedEagerUserTask(ctx, s.caseRepo, updated, task)
	}
	return updated, nil
}
