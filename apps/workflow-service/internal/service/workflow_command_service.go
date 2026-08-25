package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

type WorkflowCommandService struct {
	caseRepo *repository.CaseRepository
	zeebeSvc *ZeebeService
}

type SubmitCaseInput struct {
	Actor          string
	Variables      map[string]any
	IdempotencyKey string
}

func NewWorkflowCommandService(caseRepo *repository.CaseRepository, zeebeSvc *ZeebeService) *WorkflowCommandService {
	return &WorkflowCommandService{caseRepo: caseRepo, zeebeSvc: zeebeSvc}
}

func (s *WorkflowCommandService) CreateCase(ctx context.Context, in repository.CaseCreate) (*repository.BusinessCase, error) {
	return s.caseRepo.CreateCase(ctx, in)
}

func (s *WorkflowCommandService) SubmitCase(ctx context.Context, id string, in SubmitCaseInput) (*repository.BusinessCase, error) {
	return s.caseRepo.WithSubmissionLock(ctx, id, func(ctx context.Context) (*repository.BusinessCase, error) {
		return s.submitCaseLocked(ctx, id, in)
	})
}

func (s *WorkflowCommandService) submitCaseLocked(ctx context.Context, id string, in SubmitCaseInput) (*repository.BusinessCase, error) {
	bc, err := s.caseRepo.GetCase(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("query case: %w", err)
	}
	if bc == nil {
		return nil, repository.ErrNotFound
	}
	if in.Actor == "" {
		in.Actor = bc.CreatedBy
	}
	requestHash := submitRequestHash(id, in.Actor, in.IdempotencyKey, in.Variables)
	if bc.BpmnProcessID == nil {
		return nil, fmt.Errorf("case has no BPMN process configured")
	}
	if bc.Status != repository.CaseStatusDraft {
		if strings.TrimSpace(in.IdempotencyKey) != "" {
			if bc.SubmitIdempotencyKey == strings.TrimSpace(in.IdempotencyKey) &&
				bc.SubmitRequestHash != "" && bc.SubmitRequestHash == requestHash {
				return bc, nil
			}
			if bc.SubmitIdempotencyKey == strings.TrimSpace(in.IdempotencyKey) {
				return nil, repository.ErrIdempotencyConflict
			}
		}
		return nil, fmt.Errorf("case status must be %s", repository.CaseStatusDraft)
	}
	if s.zeebeSvc == nil {
		return nil, fmt.Errorf("zeebe service is not configured")
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
	updated, err := s.caseRepo.SubmitCase(ctx, id, in.Actor, processKey, in.IdempotencyKey, requestHash)
	if err != nil {
		return nil, fmt.Errorf("workflow started but case submit failed: %w", err)
	}
	if task, ok := InitialUserTaskForCaseType(updated.CaseType); ok {
		SeedEagerUserTask(ctx, s.caseRepo, updated, task)
	}
	return updated, nil
}

func submitRequestHash(caseID, actor, idempotencyKey string, variables map[string]any) string {
	payload := struct {
		CaseID         string         `json:"case_id"`
		Actor          string         `json:"actor"`
		IdempotencyKey string         `json:"idempotency_key"`
		Variables      map[string]any `json:"variables"`
	}{caseID, actor, strings.TrimSpace(idempotencyKey), variables}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
