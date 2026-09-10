package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/shopspring/decimal"
)

// LNM.307.01 general provision (per org): required = outstanding × rate,
// delta = required − accumulated; the approved period row is the cumulative
// source for the next period. Posting rides the LNM_PROVISION rule card
// (alloc = card lines 1-2, reverse = card lines 3-4) through finance
// PostingService, exactly like the EOD per-agreement provision.
const (
	GeneralProvisionStatusPending   = "SUBMITTED"
	GeneralProvisionStatusPosted    = "POSTED"
	GeneralProvisionStatusRejected  = "REJECTED"
	GeneralProvisionCaseType        = "LNM_GENERAL_PROVISION_V2"
	generalProvisionDocumentType    = "LNM_PROVISION"
	generalProvisionDefaultCurrency = "VND"
)

// GeneralProvisionPreview is the calculator result the maker screen renders.
type GeneralProvisionPreview struct {
	OrgCode               string  `json:"org_code"`
	ProvisionDate         string  `json:"provision_date"`
	RatePercent           float64 `json:"rate_percent"`
	TotalOutstandingMinor int64   `json:"total_outstanding_minor"`
	AccumProvisionMinor   int64   `json:"accum_provision_minor"`
	RequiredProvisionMinor int64  `json:"required_provision_minor"`
	AllocMinor            int64   `json:"alloc_minor"`
	ReverseMinor          int64   `json:"reverse_minor"`
}

// GeneralProvisionService runs the LNM.307 approval flow.
type GeneralProvisionService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
	finance  *financeclient.Client
}

func NewGeneralProvisionService(repo *repository.LoanRepository, workflow AdjustmentSubmitter, finance *financeclient.Client) *GeneralProvisionService {
	return &GeneralProvisionService{repo: repo, workflow: workflow, finance: finance}
}

// Calculate computes the period figures without persisting anything.
func (s *GeneralProvisionService) Calculate(ctx context.Context, tenantID, orgCode, asOf string) (GeneralProvisionPreview, error) {
	orgCode = strings.TrimSpace(orgCode)
	if !isValidISODate(asOf) {
		return GeneralProvisionPreview{}, ardaerrors.New(ardaerrors.CodeInvalidInput, "provision_date must be YYYY-MM-DD")
	}
	rate, err := s.repo.GeneralProvisionRate(ctx, orgCodeOrDefault(orgCode))
	if err != nil {
		return GeneralProvisionPreview{}, mapRepoError(err)
	}
	outstanding, err := s.repo.SumGeneralProvisionOutstanding(ctx, tenantID, orgCode, asOf)
	if err != nil {
		return GeneralProvisionPreview{}, mapRepoError(err)
	}
	accum, err := s.repo.LatestPostedRequired(ctx, tenantID, orgCodeOrDefault(orgCode), asOf)
	if err != nil {
		return GeneralProvisionPreview{}, mapRepoError(err)
	}
	required := requiredGeneralProvision(outstanding, rate)
	alloc, reverse := generalProvisionDelta(required, accum)
	return GeneralProvisionPreview{
		OrgCode:                orgCode,
		ProvisionDate:          asOf,
		RatePercent:            rate,
		TotalOutstandingMinor:  outstanding,
		AccumProvisionMinor:    accum,
		RequiredProvisionMinor: required,
		AllocMinor:             alloc,
		ReverseMinor:           reverse,
	}, nil
}

// Submit calculates the period, stores it SUBMITTED and starts the workflow
// case (maker step). One action per period (no DRAFT editing: the figures are
// server-computed, the screen previews them before submitting).
func (s *GeneralProvisionService) Submit(ctx context.Context, tenantID, actor, orgCode, asOf string) (*repository.GeneralProvisionRow, error) {
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	preview, err := s.Calculate(ctx, tenantID, orgCode, asOf)
	if err != nil {
		return nil, err
	}
	row := repository.GeneralProvisionRow{
		ID:               repository.NewID("lnmgp"),
		TenantID:         tenantID,
		OrgCode:          orgCodeOrDefault(preview.OrgCode),
		ProvisionDate:    preview.ProvisionDate,
		RatePercent:      preview.RatePercent,
		TotalOutstanding: preview.TotalOutstandingMinor,
		Accum:            preview.AccumProvisionMinor,
		Required:         preview.RequiredProvisionMinor,
		Alloc:            preview.AllocMinor,
		Reverse:          preview.ReverseMinor,
		Status:           GeneralProvisionStatusPending,
		CreatedBy:        actor,
	}
	if err := s.repo.InsertGeneralProvision(ctx, row); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, ardaerrors.New(ardaerrors.CodeConflict, "a general provision already exists for this org and date")
		}
		return nil, mapRepoError(err)
	}
	title := fmt.Sprintf("Trích lập dự phòng chung %s kỳ %s", row.OrgCode, row.ProvisionDate)
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          GeneralProvisionCaseType,
		Title:             title,
		PrimaryObjectType: "lnm.general_provision",
		PrimaryObjectID:   row.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("lnm-general-provision-%s-%s", row.OrgCode, row.ProvisionDate),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"generalProvisionId": row.ID,
		"orgCode":            row.OrgCode,
		"provisionDate":      row.ProvisionDate,
	}
	if _, err := s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars,
		fmt.Sprintf("lnm-general-provision-%s-submit", row.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetGeneralProvisionCase(ctx, tenantID, row.ID, caseCreated.Id, caseCreated.CaseCode); err != nil {
		return nil, mapRepoError(err)
	}
	return s.repo.GetGeneralProvision(ctx, tenantID, row.ID)
}

func (s *GeneralProvisionService) List(ctx context.Context, tenantID, orgCode string) ([]repository.GeneralProvisionRow, error) {
	items, err := s.repo.ListGeneralProvisions(ctx, tenantID, strings.TrimSpace(orgCode))
	return items, mapRepoError(err)
}

// Check validates the period is actionable for the BPMN validate job.
func (s *GeneralProvisionService) Check(ctx context.Context, tenantID, id string) (bool, string, error) {
	row, err := s.repo.GetGeneralProvision(ctx, tenantID, id)
	if err != nil {
		return false, err.Error(), nil
	}
	if row.Status != GeneralProvisionStatusPending {
		return false, fmt.Sprintf("general provision status is %s, want %s", row.Status, GeneralProvisionStatusPending), nil
	}
	return true, "ok", nil
}

// Resolve applies the checker decision: APPROVE recomputes the delta at the
// stored date, posts the LNM_PROVISION card and marks the period POSTED.
func (s *GeneralProvisionService) Resolve(ctx context.Context, tenantID, id, decision, decidedBy, note string) error {
	row, err := s.repo.GetGeneralProvision(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	if row.Status != GeneralProvisionStatusPending {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "general provision is not awaiting approval")
	}
	if decision != "APPROVE" {
		return mapRepoError(s.repo.ResolveGeneralProvision(ctx, tenantID, id, GeneralProvisionStatusRejected, decidedBy))
	}
	if s.finance == nil {
		return ardaerrors.New(ardaerrors.CodeInternal, "finance client is not configured")
	}
	preview, err := s.Calculate(ctx, tenantID, row.OrgCode, row.ProvisionDate)
	if err != nil {
		return err
	}
	journalEntryID := ""
	if preview.AllocMinor > 0 || preview.ReverseMinor > 0 {
		posted, err := s.finance.Post(ctx, s.postingRequest(row, preview))
		if err != nil {
			return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "provision posting failed", err)
		}
		journalEntryID = posted.GetJournalEntryId()
	}
	return mapRepoError(s.repo.SettleGeneralProvision(ctx, tenantID, id,
		preview.TotalOutstandingMinor, preview.AccumProvisionMinor,
		preview.RequiredProvisionMinor, preview.AllocMinor, preview.ReverseMinor,
		journalEntryID, decidedBy))
}

func (s *GeneralProvisionService) postingRequest(row *repository.GeneralProvisionRow, preview GeneralProvisionPreview) *financev1.PostingRequest {
	analytics := &financev1.Analytics{OrgUnitCode: row.OrgCode}
	legs := []financeclient.PostingLeg{}
	if preview.AllocMinor > 0 {
		legs = append(legs,
			financeclient.PostingLeg{CardLine: 1, Fallback: "LNM_PROVISION_EXPENSE", Direction: "DEBIT", AmountMinor: preview.AllocMinor, Analytics: analytics},
			financeclient.PostingLeg{CardLine: 2, Fallback: "LNM_PROVISION_LIABILITY", Direction: "CREDIT", AmountMinor: preview.AllocMinor, Analytics: analytics},
		)
	}
	if preview.ReverseMinor > 0 {
		legs = append(legs,
			financeclient.PostingLeg{CardLine: 3, Fallback: "LNM_PROVISION_LIABILITY", Direction: "DEBIT", AmountMinor: preview.ReverseMinor, Analytics: analytics},
			financeclient.PostingLeg{CardLine: 4, Fallback: "LNM_PROVISION_RELEASE", Direction: "CREDIT", AmountMinor: preview.ReverseMinor, Analytics: analytics},
		)
	}
	return &financev1.PostingRequest{
		IdempotencyKey: fmt.Sprintf("lnm-general-provision-%s-%s", row.OrgCode, row.ProvisionDate),
		AccountingDate: row.ProvisionDate,
		CurrencyCode:   generalProvisionDefaultCurrency,
		Description:    fmt.Sprintf("Trích lập dự phòng chung %s kỳ %s", row.OrgCode, row.ProvisionDate),
		BusinessReference: &financev1.BusinessReference{
			Domain:       "lnm",
			DocumentType: generalProvisionDocumentType,
			DocumentId:   row.ID,
			DocumentCode: row.OrgCode + "/" + row.ProvisionDate,
		},
		Lines: financeclient.PostingLinesFromRules(
			financeclient.FetchPostingRules(s.finance, generalProvisionDocumentType),
			legs, generalProvisionDefaultCurrency),
	}
}

// requiredGeneralProvision = outstanding × rate / 100, HALF_UP to đồng (minor).
func requiredGeneralProvision(outstandingMinor int64, ratePercent float64) int64 {
	if outstandingMinor <= 0 || ratePercent <= 0 {
		return 0
	}
	required := decimal.NewFromInt(outstandingMinor).
		Mul(decimal.NewFromFloat(ratePercent)).
		Div(decimal.NewFromInt(100)).
		Round(0)
	return required.IntPart()
}

func generalProvisionDelta(required, accum int64) (alloc, reverse int64) {
	switch {
	case required > accum:
		return required - accum, 0
	case accum > required:
		return 0, accum - required
	default:
		return 0, 0
	}
}

func orgCodeOrDefault(orgCode string) string {
	if strings.TrimSpace(orgCode) == "" {
		return "%"
	}
	return strings.TrimSpace(orgCode)
}
