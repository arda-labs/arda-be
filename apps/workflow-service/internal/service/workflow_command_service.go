package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
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
	if dropped := dropReservedCaseVariables(in.Variables); len(dropped) > 0 {
		slog.Warn("ignoring client-supplied reserved case variables",
			"caseId", bc.ID, "keys", dropped)
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
	variables := buildCaseVariables(bc, in.Actor, in.Variables)
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

// reservedCaseVariableKeys are the process variables owned by the service.
// Client input can never set them: the values are always derived from the
// persisted case record (tenant, case identity, primary object) or from the
// verified actor, so a submit body cannot impersonate another tenant, case or
// actor in the variables consumed by workers and downstream services.
var reservedCaseVariableKeys = map[string]struct{}{
	"caseId":            {},
	"caseType":          {},
	"caseCode":          {},
	"tenantId":          {},
	"domainService":     {},
	"primaryObjectType": {},
	"primaryObjectId":   {},
	"customerId":        {},
	"actorUserId":       {},
	"actor_user_id":     {},
	"createdBy":         {},
	"created_by":        {},
}

// dropReservedCaseVariables removes service-owned keys from client-supplied
// variables and returns the dropped keys (sorted) for logging. The map is
// mutated so the idempotency request hash matches the variables that are
// actually sent to Zeebe.
func dropReservedCaseVariables(variables map[string]any) []string {
	var dropped []string
	for key := range variables {
		if _, reserved := reservedCaseVariableKeys[key]; reserved {
			delete(variables, key)
			dropped = append(dropped, key)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// authoritativeCaseVariables builds the system variables for a case submit
// from the persisted case and the verified actor.
func authoritativeCaseVariables(bc *repository.BusinessCase, actor string) map[string]any {
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
	if actor = strings.TrimSpace(actor); actor != "" {
		variables["actorUserId"] = actor
		variables["createdBy"] = actor
	}
	return variables
}

// buildCaseVariables merges client variables over the authoritative system
// variables. Reserved keys are skipped and the system values are re-asserted
// last, so buildCaseVariables stays safe even if called with unsanitized input.
func buildCaseVariables(bc *repository.BusinessCase, actor string, clientVariables map[string]any) map[string]any {
	system := authoritativeCaseVariables(bc, actor)
	variables := make(map[string]any, len(system)+len(clientVariables))
	for key, value := range system {
		variables[key] = value
	}
	for key, value := range clientVariables {
		if _, reserved := reservedCaseVariableKeys[key]; reserved {
			continue
		}
		variables[key] = value
	}
	for key, value := range system {
		variables[key] = value
	}
	return variables
}
