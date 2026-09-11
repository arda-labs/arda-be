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

// LNM.306 specific provision (per-loan): required = max(outstanding −
// Σ(collateral value × deduction_ratio), 0) × debt-group rate. APPROVE posts
// the LNM_PROVISION_306 card through the finance PostingService.
const (
	SpecificProvisionStatusPending  = "SUBMITTED"
	SpecificProvisionStatusPosted   = "POSTED"
	SpecificProvisionStatusRejected = "REJECTED"
	SpecificProvisionCaseType       = "LNM_SPECIFIC_PROVISION_V1"
	specificProvisionDocumentType   = "LNM_PROVISION_306"
)

// SpecificProvisionPreview is the calculator result for the maker screen.
type SpecificProvisionPreview struct {
	ContractCode     string  `json:"contract_code"`
	AgreementCode    string  `json:"agreement_code"`
	ProvisionDate    string  `json:"provision_date"`
	DebtGroupCode    string  `json:"debt_group_code"`
	RatePercent      float64 `json:"rate_percent"`
	OutstandingMinor int64   `json:"outstanding_minor"`
	DeductionMinor   int64   `json:"deduction_minor"`
	BaseMinor        int64   `json:"base_minor"`
	AmountMinor      int64   `json:"amount_minor"`
}

// SpecificProvisionService runs the LNM.306 approval flow.
type SpecificProvisionService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
	finance  *financeclient.Client
}

func NewSpecificProvisionService(repo *repository.LoanRepository, workflow AdjustmentSubmitter, finance *financeclient.Client) *SpecificProvisionService {
	return &SpecificProvisionService{repo: repo, workflow: workflow, finance: finance}
}

// Calculate computes the per-loan provision without persisting.
func (s *SpecificProvisionService) Calculate(ctx context.Context, tenantID, agreementCode, asOf string) (SpecificProvisionPreview, error) {
	agreementCode = strings.TrimSpace(agreementCode)
	if agreementCode == "" {
		return SpecificProvisionPreview{}, ardaerrors.New(ardaerrors.CodeRequired, "agreement_code is required")
	}
	if !isValidISODate(asOf) {
		return SpecificProvisionPreview{}, ardaerrors.New(ardaerrors.CodeInvalidInput, "provision_date must be YYYY-MM-DD")
	}
	outstanding, debtGroup, contractCode, err := s.repo.AgreementProvisionBase(ctx, tenantID, agreementCode)
	if err != nil {
		return SpecificProvisionPreview{}, mapRepoError(err)
	}
	rate, err := s.repo.DebtGroupProvisionRate(ctx, debtGroup)
	if err != nil {
		return SpecificProvisionPreview{}, mapRepoError(err)
	}
	deduction, err := s.repo.CollateralDeduction(ctx, tenantID, contractCode)
	if err != nil {
		return SpecificProvisionPreview{}, mapRepoError(err)
	}
	base := outstanding - deduction
	if base < 0 {
		base = 0
	}
	return SpecificProvisionPreview{
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		ProvisionDate:    asOf,
		DebtGroupCode:    debtGroup,
		RatePercent:      rate,
		OutstandingMinor: outstanding,
		DeductionMinor:   deduction,
		BaseMinor:        base,
		AmountMinor:      requiredSpecificProvision(base, rate),
	}, nil
}

// Submit stores the calculated request and starts the maker/checker case.
func (s *SpecificProvisionService) Submit(ctx context.Context, tenantID, actor, agreementCode, asOf string) (*repository.SpecificProvisionRow, error) {
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	preview, err := s.Calculate(ctx, tenantID, agreementCode, asOf)
	if err != nil {
		return nil, err
	}
	row := repository.SpecificProvisionRow{
		ID:               repository.NewID("lnm306"),
		TenantID:         tenantID,
		ContractCode:     preview.ContractCode,
		AgreementCode:    preview.AgreementCode,
		ProvisionDate:    preview.ProvisionDate,
		OutstandingMinor: preview.OutstandingMinor,
		DebtGroupCode:    preview.DebtGroupCode,
		RatePercent:      preview.RatePercent,
		DeductionMinor:   preview.DeductionMinor,
		BaseMinor:        preview.BaseMinor,
		AmountMinor:      preview.AmountMinor,
		Status:           SpecificProvisionStatusPending,
		CreatedBy:        actor,
	}
	if err := s.repo.InsertSpecificProvision(ctx, row); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, ardaerrors.New(ardaerrors.CodeConflict, "a specific provision already exists for this agreement and date")
		}
		return nil, mapRepoError(err)
	}
	title := fmt.Sprintf("Trích lập dự phòng cụ thể %s kỳ %s", row.AgreementCode, row.ProvisionDate)
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          SpecificProvisionCaseType,
		Title:             title,
		PrimaryObjectType: "lnm.specific_provision",
		PrimaryObjectID:   row.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("lnm-specific-provision-%s-%s", row.AgreementCode, row.ProvisionDate),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err := s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"specificProvisionId": row.ID,
		"agreementCode":       row.AgreementCode,
		"provisionDate":       row.ProvisionDate,
	}, fmt.Sprintf("lnm-specific-provision-%s-submit", row.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetSpecificProvisionCase(ctx, tenantID, row.ID, caseCreated.Id, caseCreated.CaseCode); err != nil {
		return nil, mapRepoError(err)
	}
	return s.repo.GetSpecificProvision(ctx, tenantID, row.ID)
}

// List returns the request list.
func (s *SpecificProvisionService) List(ctx context.Context, tenantID, status string) ([]repository.SpecificProvisionRow, error) {
	items, err := s.repo.ListSpecificProvisions(ctx, tenantID, strings.TrimSpace(status))
	return items, mapRepoError(err)
}

// Check validates the request is actionable for the BPMN validate job.
func (s *SpecificProvisionService) Check(ctx context.Context, tenantID, id string) (bool, string, error) {
	row, err := s.repo.GetSpecificProvision(ctx, tenantID, id)
	if err != nil {
		return false, err.Error(), nil
	}
	if row.Status != SpecificProvisionStatusPending {
		return false, fmt.Sprintf("specific provision status is %s, want %s", row.Status, SpecificProvisionStatusPending), nil
	}
	return true, "ok", nil
}

// Resolve posts (APPROVE) or rejects the request.
func (s *SpecificProvisionService) Resolve(ctx context.Context, tenantID, id, decision, decidedBy, note string) error {
	row, err := s.repo.GetSpecificProvision(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	if row.Status != SpecificProvisionStatusPending {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "specific provision is not awaiting approval")
	}
	if decision != "APPROVE" {
		return mapRepoError(s.repo.ResolveSpecificProvision(ctx, tenantID, id, SpecificProvisionStatusRejected, decidedBy))
	}
	preview, err := s.Calculate(ctx, tenantID, row.AgreementCode, row.ProvisionDate)
	if err != nil {
		return err
	}
	journalEntryID := ""
	if preview.AmountMinor > 0 {
		if s.finance == nil {
			return ardaerrors.New(ardaerrors.CodeInternal, "finance client is not configured")
		}
		posted, err := s.finance.Post(ctx, s.postingRequest(row, preview))
		if err != nil {
			return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "specific provision posting failed", err)
		}
		journalEntryID = posted.GetJournalEntryId()
	}
	return mapRepoError(s.repo.SettleSpecificProvision(ctx, tenantID, id, preview.AmountMinor, journalEntryID, decidedBy))
}

func (s *SpecificProvisionService) postingRequest(row *repository.SpecificProvisionRow, preview SpecificProvisionPreview) *financev1.PostingRequest {
	analytics := &financev1.Analytics{
		ContractCode: row.ContractCode,
		DebtGroupCode: row.DebtGroupCode,
	}
	legs := []financeclient.PostingLeg{
		{CardLine: 1, Fallback: "LNM_PROVISION_EXPENSE", Direction: "DEBIT", AmountMinor: preview.AmountMinor, Analytics: analytics},
		{CardLine: 2, Fallback: "LNM_PROVISION_LIABILITY", Direction: "CREDIT", AmountMinor: preview.AmountMinor, Analytics: analytics},
	}
	return &financev1.PostingRequest{
		IdempotencyKey: fmt.Sprintf("lnm-specific-provision-%s-%s", row.AgreementCode, row.ProvisionDate),
		AccountingDate: row.ProvisionDate,
		CurrencyCode:   "VND",
		Description:    fmt.Sprintf("Trích lập dự phòng cụ thể %s kỳ %s", row.AgreementCode, row.ProvisionDate),
		BusinessReference: &financev1.BusinessReference{
			Domain:       "lnm",
			DocumentType: specificProvisionDocumentType,
			DocumentId:   row.ID,
			DocumentCode: row.AgreementCode,
		},
		Lines: financeclient.PostingLinesFromRules(
			financeclient.FetchPostingRules(s.finance, specificProvisionDocumentType),
			legs, "VND"),
	}
}

// requiredSpecificProvision = base × rate / 100, HALF_UP to đồng.
func requiredSpecificProvision(baseMinor int64, ratePercent float64) int64 {
	if baseMinor <= 0 || ratePercent <= 0 {
		return 0
	}
	required := decimal.NewFromInt(baseMinor).
		Mul(decimal.NewFromFloat(ratePercent)).
		Div(decimal.NewFromInt(100)).
		Round(0)
	return required.IntPart()
}
