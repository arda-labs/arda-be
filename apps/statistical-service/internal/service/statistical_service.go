package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/indicator"
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
	repo         *repository.StatisticalRepository
	workflow     StatisticalSubmitter
	engine       *indicator.Engine
	presentation *presentationService
}

// NewStatisticalService wires the service. The indicator engine is optional so
// submission-only deployments (and tests) keep working without it.
func NewStatisticalService(repo *repository.StatisticalRepository, workflow StatisticalSubmitter) *StatisticalService {
	return &StatisticalService{repo: repo, workflow: workflow, engine: indicator.NewEngine(repo)}
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
func (s *StatisticalService) ResolveSubmission(ctx context.Context, tenantID, id, decision, actor string, dataVersion int64) error {
	status := ""
	switch decision {
	case "APPROVE":
		status = "APPROVED"
	case "REJECT":
		status = "REJECTED"
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
	applied, err := s.repo.ResolveSubmission(ctx, tenantID, id, status, actor, dataVersion)
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

// GetSubmission returns one submission by id (workbench form host read API).
func (s *StatisticalService) GetSubmission(ctx context.Context, tenantID, id string) (*repository.ReportSubmission, error) {
	return s.findSubmission(ctx, tenantID, id)
}

// IsStaleVersion reports whether err is the stale-dossier conflict raised by a
// guarded decision (the checker approved a version that changed while the
// submission was in review). The gRPC boundary maps it to codes.Aborted.
func IsStaleVersion(err error) bool {
	return errors.Is(err, repository.ErrStaleVersion)
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

// CatalogKinds is the whitelist of QCMS catalog kinds (W5) — one kind is one
// EPAS catalog screen.
var CatalogKinds = []string{
	"indicator-type",
	"stat-code-map",
	"regulation",
	"regulation-type",
	"response-define",
	"txn-status",
	"rule-define",
	"rule-type",
	"kpi-type",
	"report-group",
	"report-param",
	"import-template",
	"import-type",
	"cmms-scenario-group",
	"cmms-scenario",
	"cmms-compliance-period",
}

func isCatalogKind(kind string) bool {
	for _, known := range CatalogKinds {
		if known == kind {
			return true
		}
	}
	return false
}

// UpsertIndicatorResult stores one computed/manual indicator value and audits
// the change. Values arrive from the compute engine or an operator; the SQL
// that produced them never reaches this layer.
func (s *StatisticalService) UpsertIndicatorResult(ctx context.Context, tenantID, actor string, in *repository.IndicatorResult) (*repository.IndicatorResult, error) {
	if in.IndicatorCode == "" || in.PeriodCode == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "indicator_code and period_code are required")
	}
	in.TenantID = tenantID
	in.CreatedBy = actor
	return s.repo.UpsertIndicatorResult(ctx, in)
}

// ListIndicatorResults returns stored values for a period.
func (s *StatisticalService) ListIndicatorResults(ctx context.Context, params repository.ListIndicatorResultsParams) ([]repository.IndicatorResult, error) {
	return s.repo.ListIndicatorResults(ctx, params)
}

// Presentation returns the service's presentation surface (chart + document
// rendering), or nil when the service was built without one.
func (s *StatisticalService) Presentation() *presentationService {
	return s.presentation
}

// SetPresentation wires the presentation surface (called from main, where the
// Gotenberg URL is known).
func (s *StatisticalService) SetPresentation(p *presentationService) {
	s.presentation = p
}

// NewPresentationService builds the presentation surface for main; the
// concrete type stays private to this package.
func NewPresentationService(statistical *StatisticalService, gotenbergURL string) (*presentationService, error) {
	return newPresentationService(statistical, gotenbergURL)
}
func (s *StatisticalService) ComputeIndicator(ctx context.Context, tenantID, actor, code string, params map[string]string) (*repository.IndicatorResult, error) {
	if s.engine == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "indicator engine is not configured")
	}
	period := params["period_code"]
	if period == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "period_code is required")
	}
	dims := map[string]string{}
	for _, name := range indicator.DimensionNames {
		if v := params["dim_"+name]; v != "" {
			dims[name] = v
		}
	}
	dimKey := indicator.DimensionKey(dims)
	resolver := func(ctx context.Context, indicatorCode, periodCode string) (*float64, error) {
		results, err := s.repo.ListIndicatorResults(ctx, repository.ListIndicatorResultsParams{
			TenantID:      tenantID,
			IndicatorCode: indicatorCode,
			PeriodCode:    periodCode,
		})
		if err != nil {
			return nil, err
		}
		// Prefer the row matching this dimension key; fall back to the total.
		for _, r := range results {
			if r.DimensionKey == dimKey && r.Value != nil {
				return r.Value, nil
			}
		}
		for _, r := range results {
			if r.DimensionKey == "" && r.Value != nil {
				return r.Value, nil
			}
		}
		return nil, nil
	}
	value, err := s.engine.ComputeSeries(ctx, tenantID, code, period, dims, resolver, 0)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	result := &repository.IndicatorResult{
		IndicatorCode: code,
		PeriodCode:    period,
		DimensionKey:  dimKey,
		Value:         &value,
		Source:        "COMPUTED",
	}
	return s.UpsertIndicatorResult(ctx, tenantID, actor, result)
}

// ReconcileAccountingIndicators re-checks every account-type indicator against
// the trial-balance fact: the trial balance must balance, and each formula must
// agree with an independent recomputation over the same accounts. It is the
// safety net for the accounting seeds — a wrong account mapping or a dropped
// term shows up as a mismatch instead of a silently wrong balance sheet.
func (s *StatisticalService) ReconcileAccountingIndicators(ctx context.Context, tenantID, periodCode string) (indicator.ReconciliationReport, error) {
	report := indicator.ReconciliationReport{PeriodCode: periodCode}
	if periodCode == "" {
		return report, ardaerrors.New(ardaerrors.CodeRequired, "period_code is required")
	}
	indicators, err := s.repo.ListIndicators(ctx, repository.ListIndicatorsParams{TenantID: tenantID})
	if err != nil {
		return report, err
	}
	formulas := map[string]json.RawMessage{}
	for _, ind := range indicators {
		if len(ind.Formula) == 0 {
			continue
		}
		formulas[ind.Code] = ind.Formula
	}
	return indicator.ReconcileIndicatorFormulas(ctx, s.repo, tenantID, periodCode, formulas)
}

// UpsertRule creates or updates one threshold rule. The indicator it watches
// must exist, so a rule cannot be pointed at something the engine cannot
// produce.
func (s *StatisticalService) UpsertRule(ctx context.Context, tenantID, actor string, in *repository.IndicatorRule) (*repository.IndicatorRule, error) {
	in.TenantID = tenantID
	in.CreatedBy = actor
	indicators, err := s.repo.ListIndicators(ctx, repository.ListIndicatorsParams{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	known := false
	for _, ind := range indicators {
		if ind.Code == in.IndicatorCode {
			known = true
			break
		}
	}
	if !known {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "indicator_code does not exist: "+in.IndicatorCode)
	}
	return s.repo.UpsertRule(ctx, in)
}

// ListRules returns the tenant's threshold rules.
func (s *StatisticalService) ListRules(ctx context.Context, tenantID string, onlyActive bool) ([]repository.IndicatorRule, error) {
	return s.repo.ListRules(ctx, tenantID, onlyActive)
}

// ListAlerts returns the tenant's indicator alerts.
func (s *StatisticalService) ListAlerts(ctx context.Context, tenantID, status, periodCode string) ([]repository.IndicatorAlert, error) {
	return s.repo.ListAlerts(ctx, tenantID, status, periodCode)
}

// AckAlert acknowledges one alert.
func (s *StatisticalService) AckAlert(ctx context.Context, tenantID, id, actor string) error {
	return s.repo.AckAlert(ctx, tenantID, id, actor)
}

// EvaluateIndicatorRules compares each active rule against the period's stored
// indicator result and records or clears the matching alert. It is the
// proactive half of the reporting layer: a breach is recorded at COB without
// anyone opening a report.
//
// Idempotent per (rule, period, slice): re-running a period updates the same
// alert row, and a recovered indicator clears its stale alert.
func (s *StatisticalService) EvaluateIndicatorRules(ctx context.Context, tenantID, periodCode string) (map[string]any, error) {
	if strings.TrimSpace(periodCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "period_code is required")
	}
	rules, err := s.repo.ListRules(ctx, tenantID, true)
	if err != nil {
		return nil, err
	}
	raised, cleared, skipped := 0, 0, 0
	for _, rule := range rules {
		results, err := s.repo.ListIndicatorResults(ctx, repository.ListIndicatorResultsParams{
			TenantID:      tenantID,
			IndicatorCode: rule.IndicatorCode,
			PeriodCode:    periodCode,
		})
		if err != nil {
			return nil, err
		}
		var value *float64
		for _, r := range results {
			if r.DimensionKey == rule.DimensionKey && r.Value != nil {
				value = r.Value
				break
			}
		}
		if value == nil {
			// The indicator has not been computed for this period: nothing to
			// judge, and no alert to clear either.
			skipped++
			continue
		}
		if compareValues(*value, rule.Operator, rule.Threshold) {
			msg := rule.Message
			if msg == "" {
				msg = fmt.Sprintf("%s %s %g (giá trị %g)", rule.Name, rule.Operator, rule.Threshold, *value)
			}
			alert, err := s.repo.UpsertAlert(ctx, &repository.IndicatorAlert{
				TenantID:      tenantID,
				RuleCode:      rule.Code,
				IndicatorCode: rule.IndicatorCode,
				DimensionKey:  rule.DimensionKey,
				PeriodCode:    periodCode,
				Value:         value,
				Threshold:     rule.Threshold,
				Operator:      rule.Operator,
				Severity:      rule.Severity,
				Message:       msg,
			})
			if err != nil {
				return nil, err
			}
			// Hand the durable alert to the outbox; the relay publishes it and
			// notification-service renders it. Failure here must not lose the
			// alert itself, so it is logged, not fatal.
			if err := s.repo.EnqueueAlertEvent(ctx, alert); err != nil {
				slog.Warn("indicator alert enqueue failed", "rule", rule.Code, "err", err)
			}
			raised++
			continue
		}
		if err := s.repo.ClearAlert(ctx, tenantID, rule.Code, periodCode, rule.DimensionKey); err != nil {
			return nil, err
		}
		cleared++
	}
	return map[string]any{
		"period_code": periodCode,
		"rules":       len(rules),
		"raised":      raised,
		"cleared":     cleared,
		"skipped":     skipped,
	}, nil
}

// compareValues applies the rule operator. The operator is a closed set
// (ValidRuleOperator), never free text.
func compareValues(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "=":
		return value == threshold
	case "<>":
		return value != threshold
	default:
		return false
	}
}

// ComputeAllIndicators evaluates every active indicator whose kpi_type is P or
// C for a period and stores the results. Indicators whose formula cannot be
// evaluated (e.g. growth without a comparison period) are reported, not
// silently skipped.
func (s *StatisticalService) ComputeAllIndicators(ctx context.Context, tenantID, actor string, params map[string]string) (map[string]any, error) {
	period := params["period_code"]
	if period == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "period_code is required")
	}
	indicators, err := s.repo.ListIndicators(ctx, repository.ListIndicatorsParams{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	// Two passes: primaries and ratios first, so growth/trailing-average
	// indicators find their base values already stored.
	computed, failed := []map[string]any{}, []map[string]any{}
	for _, ind := range indicators {
		var f struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(ind.Formula, &f)
		if f.Type == "growth" || f.Type == "trailing_average" {
			continue
		}
		value, err := s.engine.Compute(ctx, tenantID, ind.Code, period, map[string]string{})
		if err != nil {
			failed = append(failed, map[string]any{"code": ind.Code, "error": err.Error()})
			continue
		}
		if _, err := s.UpsertIndicatorResult(ctx, tenantID, actor, &repository.IndicatorResult{
			IndicatorCode: ind.Code, PeriodCode: period, Value: &value, Source: "COMPUTED",
		}); err != nil {
			failed = append(failed, map[string]any{"code": ind.Code, "error": err.Error()})
			continue
		}
		computed = append(computed, map[string]any{"code": ind.Code, "value": value})
	}
	for _, ind := range indicators {
		var f struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(ind.Formula, &f)
		if f.Type != "growth" && f.Type != "trailing_average" {
			continue
		}
		// Second pass through ComputeIndicator so the resolver sees pass 1.
		if _, err := s.ComputeIndicator(ctx, tenantID, actor, ind.Code, map[string]string{"period_code": period}); err != nil {
			failed = append(failed, map[string]any{"code": ind.Code, "error": err.Error()})
			continue
		}
		computed = append(computed, map[string]any{"code": ind.Code, "value": nil})
	}
	return map[string]any{
		"period_code": period, "computed": computed, "failed": failed,
		"computed_count": len(computed), "failed_count": len(failed),
	}, nil
}

// ListCatalogItems returns rows of one kind.
func (s *StatisticalService) ListCatalogItems(ctx context.Context, tenantID, kind, q string, includeInactive bool) ([]repository.CatalogItem, error) {
	if !isCatalogKind(kind) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown catalog kind: "+kind)
	}
	return s.repo.ListCatalogItems(ctx, tenantID, kind, q, includeInactive)
}

// UpsertCatalogItem creates or updates one catalog row.
func (s *StatisticalService) UpsertCatalogItem(ctx context.Context, tenantID, actor, kind string, in *repository.CatalogItem) (*repository.CatalogItem, error) {
	if !isCatalogKind(kind) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown catalog kind: "+kind)
	}
	if in.Code == "" || in.Name == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	in.TenantID = tenantID
	in.Kind = kind
	in.CreatedBy = actor
	created, err := s.repo.UpsertCatalogItem(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// SetCatalogItemActive toggles one catalog row.
func (s *StatisticalService) SetCatalogItemActive(ctx context.Context, tenantID, kind, id string, active bool) error {
	if !isCatalogKind(kind) {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown catalog kind: "+kind)
	}
	if err := s.repo.SetCatalogItemActive(ctx, tenantID, kind, id, active); err != nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	return nil
}

// ListFormTemplates returns the form/template catalog (W5b).
func (s *StatisticalService) ListFormTemplates(ctx context.Context, tenantID string, includeInactive bool) ([]repository.FormTemplate, error) {
	return s.repo.ListFormTemplates(ctx, tenantID, includeInactive)
}

// UpsertFormTemplate creates or updates one form template.
func (s *StatisticalService) UpsertFormTemplate(ctx context.Context, tenantID, actor string, in *repository.FormTemplate) (*repository.FormTemplate, error) {
	if in.Code == "" || in.Name == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	in.TenantID = tenantID
	in.CreatedBy = actor
	created, err := s.repo.UpsertFormTemplate(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// ExportFormTemplate renders one template as portable JSON (import elsewhere).
func (s *StatisticalService) ExportFormTemplate(ctx context.Context, tenantID, code string) ([]byte, string, error) {
	template, err := s.repo.GetFormTemplateByCode(ctx, tenantID, code)
	if err != nil {
		return nil, "", ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if template == nil {
		return nil, "", ardaerrors.New(ardaerrors.CodeNotFound, "form template not found: "+code)
	}
	payload, err := json.Marshal(map[string]any{
		"code":               template.Code,
		"name":               template.Name,
		"schema":             template.Schema,
		"workflow_case_type": template.WorkflowCaseType,
		"media_file_id":      template.MediaFileID,
	})
	if err != nil {
		return nil, "", ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return payload, template.Code + ".json", nil
}

// ImportFormTemplate upserts one template from the exported JSON shape.
func (s *StatisticalService) ImportFormTemplate(ctx context.Context, tenantID, actor string, payload []byte) (*repository.FormTemplate, error) {
	var in struct {
		Code             string          `json:"code"`
		Name             string          `json:"name"`
		Schema           json.RawMessage `json:"schema"`
		WorkflowCaseType string          `json:"workflow_case_type"`
		MediaFileID      *string         `json:"media_file_id"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "invalid template JSON")
	}
	return s.UpsertFormTemplate(ctx, tenantID, actor, &repository.FormTemplate{
		Code:             in.Code,
		Name:             in.Name,
		Schema:           in.Schema,
		WorkflowCaseType: in.WorkflowCaseType,
		MediaFileID:      in.MediaFileID,
	})
}

// Dashboard returns the QCMS activity summary (W5c).
func (s *StatisticalService) Dashboard(ctx context.Context, tenantID string) (map[string]any, error) {
	statuses, err := s.repo.SubmissionStatusCounts(ctx, tenantID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	catalogKinds, err := s.repo.CatalogKindCounts(ctx, tenantID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	definitions, indicators, forms, err := s.repo.ActiveCounts(ctx, tenantID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return map[string]any{
		"submissions_by_status": statuses,
		"catalog_by_kind":       catalogKinds,
		"report_definitions":    definitions,
		"indicators":            indicators,
		"form_templates":        forms,
	}, nil
}
