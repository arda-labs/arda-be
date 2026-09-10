package service

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/statistical-service/internal/reports"
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
func (s *StatisticalService) ListReportDefinitions(ctx context.Context, params repository.ListReportDefinitionsParams) ([]repository.ReportDefinition, error) {
	return s.repo.ListReportDefinitions(ctx, params)
}

// UpsertReportDefinition creates or updates one definition.
func (s *StatisticalService) UpsertReportDefinition(ctx context.Context, tenantID, actor string, in *repository.ReportDefinition) (*repository.ReportDefinition, error) {
	if in.Code == "" || in.Name == "" || in.QueryID == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code, name and query_id are required")
	}
	if !isKnownQueryID(in.QueryID) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown query_id: "+in.QueryID)
	}
	in.TenantID = tenantID
	in.IsActive = true
	in.CreatedBy = actor
	in.UpdatedBy = actor
	return s.repo.UpsertReportDefinition(ctx, in)
}

// isKnownQueryID reports whether the builder registry knows the query id.
func isKnownQueryID(id string) bool {
	for _, known := range reports.KnownQueryIDs() {
		if known == id {
			return true
		}
	}
	return false
}

// ListIndicators returns the indicator catalog.
func (s *StatisticalService) ListIndicators(ctx context.Context, params repository.ListIndicatorsParams) ([]repository.Indicator, error) {
	return s.repo.ListIndicators(ctx, params)
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
	if _, err := s.repo.MarkSubmissionSubmitted(ctx, tenantID, existing.ID, caseCreated.Id, actor); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeInternal, "stamp submission submitted failed", err)
	}
	existing.Status = "SUBMITTED"
	existing.SubmittedBy = actor
	existing.WorkflowCaseID = &caseCreated.Id
	return existing, nil
}

// CheckSubmission validates that the staged submission is actionable (used by
// the rpt-submit-v2 validate job).
func (s *StatisticalService) CheckSubmission(ctx context.Context, tenantID, id string) (bool, string, error) {
	sub, err := s.repo.GetSubmissionByID(ctx, tenantID, id)
	if err != nil {
		return false, "", err
	}
	if sub == nil {
		return false, "submission not found", nil
	}
	if sub.Status != "SUBMITTED" {
		return false, "status " + sub.Status + " is not actionable", nil
	}
	return true, "", nil
}

// ResolveSubmission applies the checker decision (APPROVE→APPROVED,
// REJECT→REJECTED); idempotent when the same decision arrives twice.
func (s *StatisticalService) ResolveSubmission(ctx context.Context, tenantID, id, decision, actor string) error {
	status := ""
	switch decision {
	case "APPROVE":
		status = "APPROVED"
	case "REJECT":
		status = "REJECTED"
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
	applied, err := s.repo.ResolveSubmission(ctx, tenantID, id, status, actor)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	sub, err := s.repo.GetSubmissionByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if sub == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "submission not found")
	}
	if sub.Status == status {
		return nil
	}
	return ardaerrors.New(ardaerrors.CodeInvalidInput, "submission is not SUBMITTED")
}

// findSubmission locates one submission by id.
func (s *StatisticalService) findSubmission(ctx context.Context, tenantID, id string) (*repository.ReportSubmission, error) {
	sub, err := s.repo.GetSubmissionByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "submission not found")
	}
	return sub, nil
}

// ListSubmissions passthrough for the read API.
func (s *StatisticalService) ListSubmissions(ctx context.Context, params repository.ListSubmissionsParams) ([]repository.ReportSubmission, int, error) {
	return s.repo.ListSubmissions(ctx, params)
}

// RunReport renders one report definition: validates params against the
// builder, runs the parameterised query and returns the rows.
func (s *StatisticalService) RunReport(ctx context.Context, tenantID, code string, params map[string]string) (*repository.ReportDefinition, *reports.ReportQuery, [][]any, error) {
	definition, err := s.repo.GetReportDefinitionByCode(ctx, tenantID, code)
	if err != nil {
		return nil, nil, nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if definition == nil {
		return nil, nil, nil, ardaerrors.New(ardaerrors.CodeNotFound, "report definition not found: "+code)
	}
	query, err := reports.Build(definition.QueryID, reports.Params{
		TenantID:   tenantID,
		PeriodCode: params["period_code"],
		OrgCode:    params["org_code"],
	})
	if err != nil {
		return nil, nil, nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	rows, err := s.repo.RunQuery(ctx, query.SQL, query.Args, query.Columns)
	if err != nil {
		return nil, nil, nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return definition, query, rows, nil
}

// ExportReport renders one report into XLSX bytes (download endpoint; media
// hosting was dropped in favour of direct streaming — smaller surface).
func (s *StatisticalService) ExportReport(ctx context.Context, tenantID, code string, params map[string]string) ([]byte, string, error) {
	definition, query, rows, err := s.RunReport(ctx, tenantID, code, params)
	if err != nil {
		return nil, "", err
	}
	data, err := reports.ToExcelX(definition.Name, query.Columns, rows)
	if err != nil {
		return nil, "", ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return data, definition.Code + ".xlsx", nil
}
