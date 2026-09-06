package service

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// StatisticalSubmitter is the workflow submit interface (same shape as
// loan/crm services).
type StatisticalSubmitter interface {
	CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error)
	SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error)
}

// StatisticalService runs QCMS flows: report definitions, indicators,
// and report submission cases (maker-checker via workflow).
type StatisticalService struct {
	repo     *repository.StatisticalRepository
	workflow StatisticalSubmitter
}

func NewStatisticalService(repo *repository.StatisticalRepository, workflow StatisticalSubmitter) *StatisticalService {
	return &StatisticalService{repo: repo, workflow: workflow}
}

// CaseType is the report submission case type.
const CaseType = "RPT_SUBMIT_V2"

// ListReportDefinitions returns active report definitions.
func (s *StatisticalService) ListReportDefinitions(ctx context.Context, tenantID string) ([]repository.ReportDefinition, error) {
	return s.repo.ListReportDefinitions(ctx, tenantID)
}

// UpsertReportDefinition creates or updates one definition.
func (s *StatisticalService) UpsertReportDefinition(ctx context.Context, tenantID, actor string, in *repository.ReportDefinition) (*repository.ReportDefinition, error) {
	if in.Code == "" || in.Name == "" || in.QueryID == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code, name and query_id are required")
	}
	in.TenantID = tenantID
	in.IsActive = true
	in.CreatedBy = actor
	return s.repo.UpsertReportDefinition(ctx, in)
}

// ListIndicators returns the indicator catalog.
func (s *StatisticalService) ListIndicators(ctx context.Context, tenantID string) ([]repository.Indicator, error) {
	return s.repo.ListIndicators(ctx, tenantID)
}

// UpsertIndicator creates or updates one indicator.
func (s *StatisticalService) UpsertIndicator(ctx context.Context, tenantID, actor string, in *repository.Indicator) (*repository.Indicator, error) {
	if in.Code == "" || in.Name == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	in.TenantID = tenantID
	in.IsActive = true
	in.CreatedBy = actor
	return s.repo.UpsertIndicator(ctx, in)
}

// CreateSubmission records a DRAFT submission.
func (s *StatisticalService) CreateSubmission(ctx context.Context, tenantID, actor string, in *repository.ReportSubmission) (*repository.ReportSubmission, error) {
	if in.ReportCode == "" || in.PeriodCode == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "report_code and period_code are required")
	}
	in.ID = ""
	in.TenantID = tenantID
	in.Status = "DRAFT"
	in.CreatedBy = actor
	return s.repo.CreateSubmission(ctx, in)
}

// SubmitSubmission pushes a DRAFT submission into the RPT_SUBMIT_V2 case.
func (s *StatisticalService) SubmitSubmission(ctx context.Context, tenantID, actor, id string) (*repository.ReportSubmission, error) {
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	existing, err := s.findSubmission(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if existing.Status != "DRAFT" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only DRAFT submissions can be submitted")
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseType,
		Title:             fmt.Sprintf("Nộp báo cáo %s kỳ %s", existing.ReportCode, existing.PeriodCode),
		PrimaryObjectType: "rpt.submission",
		PrimaryObjectID:   existing.ID,
		DomainService:     "statistical-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("rpt-submit-%s", existing.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"submissionId": existing.ID,
		"reportCode":   existing.ReportCode,
		"periodCode":   existing.PeriodCode,
	}, fmt.Sprintf("rpt-submit-%s-submit", existing.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	return existing, nil
}

// findSubmission locates one submission by id across the list filter.
func (s *StatisticalService) findSubmission(ctx context.Context, tenantID, id string) (*repository.ReportSubmission, error) {
	all, err := s.repo.ListSubmissions(ctx, tenantID, "", "", "")
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, ardaerrors.New(ardaerrors.CodeNotFound, "submission not found")
}

// ListSubmissions passthrough for the read API.
func (s *StatisticalService) ListSubmissions(ctx context.Context, tenantID, reportCode, periodCode, status string) ([]repository.ReportSubmission, error) {
	return s.repo.ListSubmissions(ctx, tenantID, reportCode, periodCode, status)
}
