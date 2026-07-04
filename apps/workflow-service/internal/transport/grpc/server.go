package grpc

import (
	"context"
	"errors"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type WorkflowServer struct {
	workflowv1.UnimplementedWorkflowCommandServiceServer
	commands *service.WorkflowCommandService
}

func NewWorkflowServer(commands *service.WorkflowCommandService) *WorkflowServer {
	return &WorkflowServer{commands: commands}
}

func (s *WorkflowServer) CreateCase(ctx context.Context, req *workflowv1.CreateCaseRequest) (*workflowv1.BusinessCase, error) {
	bc, err := s.commands.CreateCase(ctx, repository.CaseCreate{
		TenantID:          req.GetTenantId(),
		CaseType:          req.GetCaseType(),
		CaseCode:          req.GetCaseCode(),
		Title:             req.GetTitle(),
		PrimaryObjectType: req.GetPrimaryObjectType(),
		PrimaryObjectID:   req.GetPrimaryObjectId(),
		DomainService:     req.GetDomainService(),
		Priority:          req.GetPriority(),
		CreatedBy:         req.GetCreatedBy(),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return businessCaseProto(bc), nil
}

func (s *WorkflowServer) SubmitCase(ctx context.Context, req *workflowv1.SubmitCaseRequest) (*workflowv1.BusinessCase, error) {
	var variables map[string]any
	if req.GetVariables() != nil {
		variables = req.GetVariables().AsMap()
	}
	bc, err := s.commands.SubmitCase(ctx, req.GetCaseId(), service.SubmitCaseInput{
		Actor:     req.GetActor(),
		Variables: variables,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}
	return businessCaseProto(bc), nil
}

func businessCaseProto(bc *repository.BusinessCase) *workflowv1.BusinessCase {
	if bc == nil {
		return nil
	}
	out := &workflowv1.BusinessCase{
		Id:                bc.ID,
		TenantId:          bc.TenantID,
		CaseType:          bc.CaseType,
		CaseCode:          bc.CaseCode,
		Title:             bc.Title,
		PrimaryObjectType: bc.PrimaryObjectType,
		PrimaryObjectId:   bc.PrimaryObjectID,
		DomainService:     bc.DomainService,
		Status:            bc.Status,
		CurrentStep:       bc.CurrentStep,
		Priority:          bc.Priority,
		CreatedBy:         bc.CreatedBy,
		CreatedAt:         timestamppb.New(bc.CreatedAt),
		UpdatedAt:         timestamppb.New(bc.UpdatedAt),
	}
	if bc.AssignedTo != nil {
		out.AssignedTo = *bc.AssignedTo
	}
	if bc.CandidateRole != nil {
		out.CandidateRole = *bc.CandidateRole
	}
	if bc.SLAPolicyID != nil {
		out.SlaPolicyId = *bc.SLAPolicyID
	}
	if bc.SLADueAt != nil {
		out.SlaDueAt = timestamppb.New(*bc.SLADueAt)
	}
	if bc.ProcessInstanceKey != nil {
		out.ProcessInstanceKey = *bc.ProcessInstanceKey
	}
	if bc.BpmnProcessID != nil {
		out.BpmnProcessId = *bc.BpmnProcessID
	}
	if bc.BpmnVersion != nil {
		out.BpmnVersion = int32(*bc.BpmnVersion)
	}
	if bc.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*bc.CompletedAt)
	}
	return out
}
