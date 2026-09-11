package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// CapitalSubmitter is the workflow submit surface (same shape as other domains).
type CapitalSubmitter interface {
	CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error)
	SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error)
}

// Case types + request kinds used by the stage/resolve callbacks.
const (
	CaseContractFormation = "CFC_CONTRACT_V1"
	CaseContractAmendment = "CFC_AMENDMENT_V1"
	CaseMovement          = "CFC_MOVEMENT_V1"

	KindContractFormation = "CONTRACT_FORMATION"
	KindContractAmendment = "CONTRACT_AMENDMENT"
	KindMovement          = "MOVEMENT"
)

// ContractDetail is the aggregate read model for the contract detail screen.
type ContractDetail struct {
	Contract    *repository.CapitalContract    `json:"contract"`
	FundType    *repository.FundType           `json:"fund_type,omitempty"`
	Product     *repository.CapitalProduct     `json:"product,omitempty"`
	Movements   []repository.CapitalMovement   `json:"movements"`
	Amendments  []repository.ContractAmendment `json:"amendments"`
}

// CapitalService runs CFM fund flows: formation/amendments and movements all
// stage through workflow-service cases; posting happens on checker APPROVE via
// the finance PostingService (provisional cards CFC_*, chờ dev_fac).
type CapitalService struct {
	repo     *repository.CapitalRepository
	db       *sql.DB
	finance  *financeclient.Client
	workflow CapitalSubmitter
}

func NewCapitalService(repo *repository.CapitalRepository, db *sql.DB, finance *financeclient.Client, workflow CapitalSubmitter) *CapitalService {
	return &CapitalService{repo: repo, db: db, finance: finance, workflow: workflow}
}

// ── Fund types + products ──

// ListFundTypes returns fund types (includeInactive for the admin catalog).
func (s *CapitalService) ListFundTypes(ctx context.Context, tenantID string, includeInactive bool) ([]repository.FundType, error) {
	return s.repo.ListFundTypes(ctx, tenantID, includeInactive)
}

// CreateFundType inserts one fund type.
func (s *CapitalService) CreateFundType(ctx context.Context, tenantID, actor string, in *repository.FundType) (*repository.FundType, error) {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	in.ID = ""
	in.TenantID = tenantID
	in.IsActive = true
	in.CreatedBy = actor
	created, err := s.repo.CreateFundType(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// UpdateFundType updates name/is_active.
func (s *CapitalService) UpdateFundType(ctx context.Context, tenantID, actor string, in *repository.FundType) (*repository.FundType, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "id is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "name is required")
	}
	in.TenantID = tenantID
	updated, err := s.repo.UpdateFundType(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	return updated, nil
}

// DeactivateFundType soft-deletes one fund type.
func (s *CapitalService) DeactivateFundType(ctx context.Context, tenantID, id string) error {
	if err := s.repo.SetFundTypeActive(ctx, tenantID, id, false); err != nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	return nil
}

// ListProducts returns fund products.
func (s *CapitalService) ListProducts(ctx context.Context, tenantID string, includeInactive bool) ([]repository.CapitalProduct, error) {
	return s.repo.ListProducts(ctx, tenantID, includeInactive)
}

// UpsertProduct creates or updates a fund product.
func (s *CapitalService) UpsertProduct(ctx context.Context, tenantID, actor string, in *repository.CapitalProduct) (*repository.CapitalProduct, error) {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.FundTypeCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code, name and fund_type_code are required")
	}
	ft, err := s.repo.GetFundTypeByCode(ctx, tenantID, in.FundTypeCode)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if ft == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "fund_type_code does not exist")
	}
	in.ID = ""
	in.TenantID = tenantID
	in.IsActive = true
	in.CreatedBy = actor
	created, err := s.repo.UpsertProduct(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// ── Contracts ──

// ListContracts returns fund contracts.
func (s *CapitalService) ListContracts(ctx context.Context, params repository.ListContractsParams) ([]repository.CapitalContract, int, error) {
	return s.repo.ListContracts(ctx, params)
}

// GetContractDetail returns the contract aggregate (contract + movements +
// amendments + catalog names) for the detail screen.
func (s *CapitalService) GetContractDetail(ctx context.Context, tenantID, id string) (*ContractDetail, error) {
	contract, err := s.repo.GetContractByID(ctx, tenantID, id)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if contract == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "contract not found")
	}
	detail := &ContractDetail{Contract: contract}
	if ft, err := s.repo.GetFundTypeByCode(ctx, tenantID, contract.FundTypeCode); err == nil {
		detail.FundType = ft
	}
	if contract.ProductCode != "" {
		if p, err := s.repo.GetProductByCode(ctx, tenantID, contract.ProductCode); err == nil {
			detail.Product = p
		}
	}
	movements, err := s.repo.ListMovementsByContract(ctx, tenantID, id)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	amendments, err := s.repo.ListAmendmentsByContract(ctx, tenantID, id)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	detail.Movements = movements
	detail.Amendments = amendments
	return detail, nil
}

// CreateContract stages a formation request (CFC_CONTRACT_V1) — the contract
// becomes ACTIVE only after checker APPROVE.
func (s *CapitalService) CreateContract(ctx context.Context, tenantID, actor string, in *repository.CapitalContract) (*repository.CapitalContract, error) {
	if strings.TrimSpace(in.ContractCode) == "" || strings.TrimSpace(in.FundTypeCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "contract_code and fund_type_code are required")
	}
	if in.AmountMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_minor must be positive")
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	ft, err := s.repo.GetFundTypeByCode(ctx, tenantID, in.FundTypeCode)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if ft == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "fund_type_code does not exist")
	}
	in.TenantID = tenantID
	in.Status = "PENDING_APPROVAL"
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	in.CreatedBy = actor
	created, err := s.repo.CreateContract(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseContractFormation,
		Title:             fmt.Sprintf("Trình hợp đồng vốn %s", created.ContractCode),
		PrimaryObjectType: "cfc.contract",
		PrimaryObjectID:   created.ID,
		DomainService:     "capital-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("cfc-contract-%s", created.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"contractId":   created.ID,
		"contractCode": created.ContractCode,
	}, fmt.Sprintf("cfc-contract-%s-submit", created.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetContractCase(ctx, tenantID, created.ID, caseCreated.Id, actor); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	created.WorkflowCaseID = &caseCreated.Id
	return created, nil
}

// SubmitAmendment stages a contract amendment (CFC_AMENDMENT_V1).
func (s *CapitalService) SubmitAmendment(ctx context.Context, tenantID, actor, contractID string, payload json.RawMessage, reason string) (*repository.ContractAmendment, error) {
	if len(payload) == 0 {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "payload is required")
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	contract, err := s.repo.GetContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if contract == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "contract not found")
	}
	if contract.Status != "ACTIVE" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only ACTIVE contracts can be amended")
	}
	amendment := &repository.ContractAmendment{
		TenantID:   tenantID,
		ContractID: contractID,
		Status:     "DRAFT",
		Payload:    payload,
		Reason:     reason,
		CreatedBy:  actor,
	}
	created, err := s.repo.CreateAmendment(ctx, amendment)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseContractAmendment,
		Title:             fmt.Sprintf("Điều chỉnh hợp đồng vốn %s", contract.ContractCode),
		PrimaryObjectType: "cfc.amendment",
		PrimaryObjectID:   created.ID,
		DomainService:     "capital-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("cfc-amendment-%s", created.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"amendmentId":  created.ID,
		"contractId":   contractID,
		"contractCode": contract.ContractCode,
	}, fmt.Sprintf("cfc-amendment-%s-submit", created.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetAmendmentCase(ctx, tenantID, created.ID, caseCreated.Id, actor); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	created.Status = "SUBMITTED"
	created.WorkflowCaseID = &caseCreated.Id
	return created, nil
}

// RecordMovement stages a movement request (CFC_MOVEMENT_V1) for one of
// RECEIPT / DISBURSEMENT / PAYMENT / SETTLEMENT. Posting happens on APPROVE.
func (s *CapitalService) RecordMovement(ctx context.Context, tenantID, actor string, in *repository.CapitalMovement) (*repository.CapitalMovement, error) {
	in.MovementType = strings.ToUpper(strings.TrimSpace(in.MovementType))
	switch in.MovementType {
	case "RECEIPT", "DISBURSEMENT", "PAYMENT", "SETTLEMENT":
	default:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "movement_type must be RECEIPT, DISBURSEMENT, PAYMENT or SETTLEMENT")
	}
	if in.AmountMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_minor must be positive")
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	contract, err := s.repo.GetContractByID(ctx, tenantID, in.ContractID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if contract == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "contract not found")
	}
	if contract.Status != "ACTIVE" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only ACTIVE contracts accept movements")
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = contract.CurrencyCode
	}
	if in.MovementDate == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "movement_date is required")
	}
	if err := s.checkAvailability(ctx, tenantID, contract.ID, in.MovementType, in.AmountMinor); err != nil {
		return nil, err
	}
	in.TenantID = tenantID
	in.ID = repository.NewCapitalID("cfcmv")
	in.Status = "DRAFT"
	in.IdempotencyKey = fmt.Sprintf("cfc-%s-%s", strings.ToLower(in.MovementType), in.ID)
	in.CreatedBy = actor
	created, err := s.repo.CreateMovement(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseMovement,
		Title:             fmt.Sprintf("%s hợp đồng vốn %s", movementTitle(in.MovementType), contract.ContractCode),
		PrimaryObjectType: "cfc.movement",
		PrimaryObjectID:   created.ID,
		DomainService:     "capital-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("cfc-movement-%s", created.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"movementId":   created.ID,
		"movementType": in.MovementType,
		"contractId":   contract.ID,
		"contractCode": contract.ContractCode,
	}, fmt.Sprintf("cfc-movement-%s-submit", created.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetMovementCase(ctx, tenantID, created.ID, caseCreated.Id, actor); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	created.Status = "SUBMITTED"
	created.WorkflowCaseID = &caseCreated.Id
	return created, nil
}

// ── Workflow callbacks ──

// CheckRequest validates the staged object for the case validate step.
func (s *CapitalService) CheckRequest(ctx context.Context, tenantID, kind, refID string) (bool, string, error) {
	switch kind {
	case KindContractFormation:
		contract, err := s.repo.GetContractByID(ctx, tenantID, refID)
		if err != nil {
			return false, "", err
		}
		if contract == nil {
			return false, "contract not found", nil
		}
		if contract.Status != "PENDING_APPROVAL" {
			return false, "status " + contract.Status + " is not actionable", nil
		}
		return true, "", nil
	case KindContractAmendment:
		amendment, err := s.repo.GetAmendmentByID(ctx, tenantID, refID)
		if err != nil {
			return false, "", err
		}
		if amendment == nil {
			return false, "amendment not found", nil
		}
		if amendment.Status != "SUBMITTED" {
			return false, "status " + amendment.Status + " is not actionable", nil
		}
		return true, "", nil
	case KindMovement:
		movement, err := s.repo.GetMovementByID(ctx, tenantID, refID)
		if err != nil {
			return false, "", err
		}
		if movement == nil {
			return false, "movement not found", nil
		}
		if movement.Status != "SUBMITTED" {
			return false, "status " + movement.Status + " is not actionable", nil
		}
		if err := s.checkAvailability(ctx, tenantID, movement.ContractID, movement.MovementType, movement.AmountMinor); err != nil {
			return false, err.Error(), nil
		}
		return true, "", nil
	default:
		return false, "unknown kind " + kind, nil
	}
}

// ResolveRequest applies the checker decision for the staged object.
func (s *CapitalService) ResolveRequest(ctx context.Context, tenantID, kind, refID, decision, actor string) error {
	switch kind {
	case KindContractFormation:
		return s.resolveContract(ctx, tenantID, refID, decision, actor)
	case KindContractAmendment:
		return s.resolveAmendment(ctx, tenantID, refID, decision, actor)
	case KindMovement:
		return s.resolveMovement(ctx, tenantID, refID, decision, actor)
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown kind "+kind)
	}
}

func (s *CapitalService) resolveContract(ctx context.Context, tenantID, id, decision, actor string) error {
	contract, err := s.repo.GetContractByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if contract == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "contract not found")
	}
	switch decision {
	case "APPROVE":
		if contract.Status == "ACTIVE" {
			return nil
		}
		if contract.Status != "PENDING_APPROVAL" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "contract is not PENDING_APPROVAL")
		}
		return s.repo.UpdateContractStatus(ctx, tenantID, id, "ACTIVE", actor)
	case "REJECT":
		if contract.Status == "REJECTED" {
			return nil
		}
		if contract.Status != "PENDING_APPROVAL" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "contract is not PENDING_APPROVAL")
		}
		return s.repo.UpdateContractStatus(ctx, tenantID, id, "REJECTED", actor)
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
}

type amendmentPayload struct {
	AmountMinor  *int64   `json:"amount_minor"`
	InterestRate *float64 `json:"interest_rate"`
	ContractDate *string  `json:"contract_date"`
	MaturityDate *string  `json:"maturity_date"`
	Note         string   `json:"note"`
}

func (s *CapitalService) resolveAmendment(ctx context.Context, tenantID, id, decision, actor string) error {
	amendment, err := s.repo.GetAmendmentByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if amendment == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "amendment not found")
	}
	switch decision {
	case "APPROVE":
		if amendment.Status == "APPLIED" {
			return nil
		}
		if amendment.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "amendment is not SUBMITTED")
		}
		var payload amendmentPayload
		if len(amendment.Payload) > 0 {
			if err := json.Unmarshal(amendment.Payload, &payload); err != nil {
				return ardaerrors.New(ardaerrors.CodeInvalidInput, "invalid amendment payload")
			}
		}
		if err := s.repo.ApplyContractAmendment(ctx, tenantID, amendment.ContractID, repository.AmendmentFields{
			AmountMinor:  payload.AmountMinor,
			InterestRate: payload.InterestRate,
			ContractDate: payload.ContractDate,
			MaturityDate: payload.MaturityDate,
			Note:         payload.Note,
		}, actor); err != nil {
			return err
		}
		return s.repo.SetAmendmentStatus(ctx, tenantID, id, "APPLIED", actor)
	case "REJECT":
		if amendment.Status == "REJECTED" {
			return nil
		}
		if amendment.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "amendment is not SUBMITTED")
		}
		return s.repo.SetAmendmentStatus(ctx, tenantID, id, "REJECTED", actor)
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
}

func (s *CapitalService) resolveMovement(ctx context.Context, tenantID, id, decision, actor string) error {
	movement, err := s.repo.GetMovementByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if movement == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "movement not found")
	}
	switch decision {
	case "APPROVE":
		if movement.Status == "POSTED" {
			return nil
		}
		if movement.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "movement is not SUBMITTED")
		}
		if err := s.checkAvailability(ctx, tenantID, movement.ContractID, movement.MovementType, movement.AmountMinor); err != nil {
			return err
		}
		contract, err := s.repo.GetContractByID(ctx, tenantID, movement.ContractID)
		if err != nil {
			return err
		}
		if contract == nil {
			return ardaerrors.New(ardaerrors.CodeNotFound, "contract not found")
		}
		entryID, err := s.postMovement(ctx, contract, movement)
		if err != nil {
			return err
		}
		if err := s.repo.SetMovementJournal(ctx, tenantID, movement.ID, "POSTED", entryID, actor); err != nil {
			return err
		}
		if movement.MovementType == "SETTLEMENT" {
			return s.repo.UpdateContractStatus(ctx, tenantID, contract.ID, "CLOSED", actor)
		}
		return nil
	case "REJECT":
		if movement.Status == "REJECTED" {
			return nil
		}
		if movement.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "movement is not SUBMITTED")
		}
		return s.repo.SetMovementStatus(ctx, tenantID, movement.ID, "REJECTED", actor)
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
}

// ── Internals ──

// checkAvailability enforces that outflows never exceed POSTED receipts.
func (s *CapitalService) checkAvailability(ctx context.Context, tenantID, contractID, movementType string, amountMinor int64) error {
	if movementType == "RECEIPT" {
		return nil
	}
	receipts, outflows, err := s.repo.PostedMovementTotals(ctx, tenantID, contractID)
	if err != nil {
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if amountMinor > receipts-outflows {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "insufficient contract balance")
	}
	return nil
}

func (s *CapitalService) postMovement(ctx context.Context, contract *repository.CapitalContract, movement *repository.CapitalMovement) (string, error) {
	if s.finance == nil {
		return "", ardaerrors.New(ardaerrors.CodeInternal, "finance client is not configured")
	}
	docType, debitClass, creditClass := movementCard(movement.MovementType)
	posted, err := s.finance.Post(ctx, &financev1.PostingRequest{
		IdempotencyKey: movement.IdempotencyKey,
		AccountingDate: movement.MovementDate,
		CurrencyCode:   movement.CurrencyCode,
		Description:    fmt.Sprintf("%s %s", movementTitle(movement.MovementType), contract.ContractCode),
		BusinessReference: &financev1.BusinessReference{
			Domain:       "cfc",
			DocumentType: docType,
			DocumentId:   movement.ID,
			DocumentCode: contract.ContractCode,
		},
		Lines: []*financev1.PostingLine{
			{
				LineNo:       1,
				Direction:    "DEBIT",
				AmountMinor:  movement.AmountMinor,
				CurrencyCode: movement.CurrencyCode,
				Analytics: &financev1.Analytics{
					AccClassification: debitClass,
					OrgUnitCode:       contract.OrgCode,
					FundSourceCode:    contract.FundTypeCode,
					Dimensions:        map[string]string{"contract_code": contract.ContractCode},
				},
			},
			{
				LineNo:       2,
				Direction:    "CREDIT",
				AmountMinor:  movement.AmountMinor,
				CurrencyCode: movement.CurrencyCode,
				Analytics: &financev1.Analytics{
					AccClassification: creditClass,
					OrgUnitCode:       contract.OrgCode,
					FundSourceCode:    contract.FundTypeCode,
					Dimensions:        map[string]string{"contract_code": contract.ContractCode},
				},
			},
		},
	})
	if err != nil {
		return "", ardaerrors.Wrap(ardaerrors.CodeBadGateway, "capital posting failed", err)
	}
	return posted.GetJournalEntryId(), nil
}

func movementCard(movementType string) (string, string, string) {
	switch movementType {
	case "DISBURSEMENT":
		return "CFC_DISBURSEMENT", "FUND_CAPITAL_LIABILITY", "CASH_SETTLEMENT_ACCOUNT"
	case "PAYMENT":
		return "CFC_PAYMENT", "CFC_INTEREST_EXPENSE", "CASH_SETTLEMENT_ACCOUNT"
	case "SETTLEMENT":
		return "CFC_SETTLEMENT", "FUND_CAPITAL_LIABILITY", "CASH_SETTLEMENT_ACCOUNT"
	default:
		return "CFC_RECEIPT", "CASH_SETTLEMENT_ACCOUNT", "FUND_CAPITAL_LIABILITY"
	}
}

func movementTitle(movementType string) string {
	switch movementType {
	case "DISBURSEMENT":
		return "Giải ngân vốn"
	case "PAYMENT":
		return "Thanh toán hợp đồng vốn"
	case "SETTLEMENT":
		return "Tất toán hợp đồng vốn"
	default:
		return "Tiếp nhận vốn"
	}
}

// FundSourceStatement is the sổ nguồn vốn report (W4c).
func (s *CapitalService) FundSourceStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]repository.FundSourceStatementRow, error) {
	return s.repo.FundSourceStatement(ctx, tenantID, fromDate, toDate, status)
}

// FundSourceTransactions is the giao dịch nguồn vốn report (W4c).
func (s *CapitalService) FundSourceTransactions(ctx context.Context, tenantID, fromDate, toDate, movementType string) ([]repository.FundSourceTxnRow, error) {
	return s.repo.FundSourceTransactions(ctx, tenantID, fromDate, toDate, movementType)
}
