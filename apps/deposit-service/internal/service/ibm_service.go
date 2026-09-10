package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// IBM case types (EPAS IBM.200.01 / 300.01 / 301.01 / 302.01 / 304.01).
const (
	CaseIBMPlace    = "IBM_PLACE_V1"
	CaseIBMTopUp    = "IBM_TOP_UP_V1"
	CaseIBMInterest = "IBM_INTEREST_V1"
	CaseIBMExpected = "IBM_EXPECTED_V1"
	CaseIBMWithdraw = "IBM_WITHDRAW_V1"

	// IBM request kinds (mirror the proto kind values).
	IBMKindPlace    = "PLACE"
	IBMKindTopUp    = "TOP_UP"
	IBMKindInterest = "INTEREST"
	IBMKindExpected = "EXPECTED"
	IBMKindWithdraw = "WITHDRAW"
)

// IBMDetail is the aggregate read model for the interbank contract detail.
type IBMDetail struct {
	Deposit   *repository.InterbankDeposit `json:"deposit"`
	Movements []repository.IBMMovement     `json:"movements"`
}

// IBMService runs the interbank deposit lifecycle: contract placement and
// movements (top-up / interest / expected / withdrawal) stage through
// workflow-service cases; posting happens on checker APPROVE.
type IBMService struct {
	repo     *repository.DepositRepository
	db       *sql.DB
	finance  *financeclient.Client
	workflow WorkflowSubmitter
}

func NewIBMService(repo *repository.DepositRepository, db *sql.DB, finance *financeclient.Client, workflow WorkflowSubmitter) *IBMService {
	return &IBMService{repo: repo, db: db, finance: finance, workflow: workflow}
}

// ── Products ──

// ListIBMProducts returns the interbank product catalog.
func (s *IBMService) ListIBMProducts(ctx context.Context, tenantID string, includeInactive bool) ([]repository.IBMProduct, error) {
	return s.repo.ListIBMProducts(ctx, tenantID, includeInactive)
}

// UpsertIBMProduct creates or updates one interbank product.
func (s *IBMService) UpsertIBMProduct(ctx context.Context, tenantID, actor string, in *repository.IBMProduct) (*repository.IBMProduct, error) {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	in.ID = ""
	in.TenantID = tenantID
	in.IsActive = true
	in.CreatedBy = actor
	created, err := s.repo.UpsertIBMProduct(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// ── Contracts ──

// GetIBMDetail returns the contract aggregate (contract + movements).
func (s *IBMService) GetIBMDetail(ctx context.Context, tenantID, id string) (*IBMDetail, error) {
	deposit, err := s.repo.GetInterbankDepositByID(ctx, tenantID, id)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if deposit == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "interbank deposit not found")
	}
	movements, err := s.repo.ListIBMMovementsByDeposit(ctx, tenantID, id)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return &IBMDetail{Deposit: deposit, Movements: movements}, nil
}

// SubmitPlace stages a contract placement (IBM.200.01) — ACTIVE only after
// checker APPROVE posts IBM_PLACE.
func (s *IBMService) SubmitPlace(ctx context.Context, tenantID, actor string, in *repository.InterbankDeposit) (*repository.InterbankDeposit, error) {
	if strings.TrimSpace(in.DepositCode) == "" || strings.TrimSpace(in.CounterpartyCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "deposit_code and counterparty_code are required")
	}
	if in.PrincipalMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "principal_minor must be positive")
	}
	if in.DepositDate == "" || in.MaturityDate == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "deposit_date and maturity_date are required")
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	if in.ProductCode != "" {
		product, err := s.repo.GetIBMProductByCode(ctx, tenantID, in.ProductCode)
		if err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		if product == nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "product_code does not exist")
		}
	}
	in.TenantID = tenantID
	in.Status = "PENDING_APPROVAL"
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	in.CreatedBy = actor
	created, err := s.repo.CreateInterbankDeposit(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseIBMPlace,
		Title:             fmt.Sprintf("Mở hợp đồng tiền gửi liên ngân hàng %s", created.DepositCode),
		PrimaryObjectType: "ibm.deposit",
		PrimaryObjectID:   created.ID,
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("ibm-place-%s", created.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"depositId":   created.ID,
		"depositCode": created.DepositCode,
	}, fmt.Sprintf("ibm-place-%s-submit", created.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetInterbankDepositCase(ctx, tenantID, created.ID, caseCreated.Id, actor); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	created.Status = "PENDING_APPROVAL"
	created.WorkflowCaseID = &caseCreated.Id
	return created, nil
}

// SubmitMovement stages one movement (TOP_UP / INTEREST / EXPECTED / WITHDRAW).
func (s *IBMService) SubmitMovement(ctx context.Context, tenantID, actor, depositID, kind string, amountMinor int64, movementDate, periodFrom, periodTo, note string) (*repository.IBMMovement, error) {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	caseType := ""
	switch kind {
	case IBMKindTopUp:
		caseType = CaseIBMTopUp
	case IBMKindInterest:
		caseType = CaseIBMInterest
	case IBMKindExpected:
		caseType = CaseIBMExpected
	case IBMKindWithdraw:
		caseType = CaseIBMWithdraw
	default:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "kind must be TOP_UP, INTEREST, EXPECTED or WITHDRAW")
	}
	if amountMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_minor must be positive")
	}
	if movementDate == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "movement_date is required")
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	deposit, err := s.repo.GetInterbankDepositByID(ctx, tenantID, depositID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if deposit == nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "interbank deposit not found")
	}
	if deposit.Status != "ACTIVE" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only ACTIVE contracts accept movements")
	}
	if kind == IBMKindWithdraw && amountMinor > deposit.PrincipalMinor+deposit.AccruedMinor {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "insufficient deposit balance")
	}
	movement := &repository.IBMMovement{
		TenantID:       tenantID,
		DepositID:      depositID,
		Kind:           kind,
		AmountMinor:    amountMinor,
		CurrencyCode:   deposit.CurrencyCode,
		MovementDate:   movementDate,
		PeriodFrom:     periodFrom,
		PeriodTo:       periodTo,
		Note:           note,
		Status:         "DRAFT",
		IdempotencyKey: "",
		CreatedBy:      actor,
	}
	movement.ID = repository.NewDepositID("ibmmv")
	movement.IdempotencyKey = fmt.Sprintf("ibm-%s-%s", strings.ToLower(kind), movement.ID)
	created, err := s.repo.CreateIBMMovement(ctx, movement)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          caseType,
		Title:             fmt.Sprintf("%s hợp đồng %s", ibmMovementTitle(kind), deposit.DepositCode),
		PrimaryObjectType: "ibm.movement",
		PrimaryObjectID:   created.ID,
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("ibm-movement-%s", created.ID),
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, map[string]any{
		"movementId":  created.ID,
		"kind":        kind,
		"depositId":   deposit.ID,
		"depositCode": deposit.DepositCode,
	}, fmt.Sprintf("ibm-movement-%s-submit", created.ID)); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetIBMMovementCase(ctx, tenantID, created.ID, caseCreated.Id, actor); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	created.Status = "SUBMITTED"
	created.WorkflowCaseID = &caseCreated.Id
	return created, nil
}

// ── Workflow callbacks ──

// CheckIBMRequest validates the staged object for the case validate step.
func (s *IBMService) CheckIBMRequest(ctx context.Context, tenantID, kind, refID string) (bool, string, error) {
	switch strings.ToUpper(kind) {
	case IBMKindPlace:
		deposit, err := s.repo.GetInterbankDepositByID(ctx, tenantID, refID)
		if err != nil {
			return false, "", err
		}
		if deposit == nil {
			return false, "interbank deposit not found", nil
		}
		if deposit.Status != "PENDING_APPROVAL" {
			return false, "status " + deposit.Status + " is not actionable", nil
		}
		return true, "", nil
	case IBMKindTopUp, IBMKindInterest, IBMKindExpected, IBMKindWithdraw:
		movement, err := s.repo.GetIBMMovementByID(ctx, tenantID, refID)
		if err != nil {
			return false, "", err
		}
		if movement == nil {
			return false, "movement not found", nil
		}
		if movement.Kind != strings.ToUpper(kind) {
			return false, "movement kind mismatch", nil
		}
		if movement.Status != "SUBMITTED" {
			return false, "status " + movement.Status + " is not actionable", nil
		}
		if movement.Kind == IBMKindWithdraw {
			deposit, err := s.repo.GetInterbankDepositByID(ctx, tenantID, movement.DepositID)
			if err != nil {
				return false, "", err
			}
			if deposit == nil || movement.AmountMinor > deposit.PrincipalMinor+deposit.AccruedMinor {
				return false, "insufficient deposit balance", nil
			}
		}
		return true, "", nil
	default:
		return false, "unknown kind " + kind, nil
	}
}

// ResolveIBMRequest applies the checker decision for the staged object.
func (s *IBMService) ResolveIBMRequest(ctx context.Context, tenantID, kind, refID, decision, actor string) error {
	switch strings.ToUpper(kind) {
	case IBMKindPlace:
		return s.resolvePlace(ctx, tenantID, refID, decision, actor)
	case IBMKindTopUp, IBMKindInterest, IBMKindExpected, IBMKindWithdraw:
		return s.resolveMovement(ctx, tenantID, refID, strings.ToUpper(kind), decision, actor)
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown kind "+kind)
	}
}

func (s *IBMService) resolvePlace(ctx context.Context, tenantID, id, decision, actor string) error {
	deposit, err := s.repo.GetInterbankDepositByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if deposit == nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "interbank deposit not found")
	}
	switch decision {
	case "APPROVE":
		if deposit.Status == "ACTIVE" {
			return nil
		}
		if deposit.Status != "PENDING_APPROVAL" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "contract is not PENDING_APPROVAL")
		}
		if err := s.postIBM(ctx, deposit, nil, "IBM_PLACE"); err != nil {
			return err
		}
		return s.repo.UpdateInterbankDepositStatus(ctx, tenantID, id, "ACTIVE", actor)
	case "REJECT":
		if deposit.Status == "REJECTED" {
			return nil
		}
		if deposit.Status != "PENDING_APPROVAL" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "contract is not PENDING_APPROVAL")
		}
		return s.repo.UpdateInterbankDepositStatus(ctx, tenantID, id, "REJECTED", actor)
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
}

func (s *IBMService) resolveMovement(ctx context.Context, tenantID, id, kind, decision, actor string) error {
	movement, err := s.repo.GetIBMMovementByID(ctx, tenantID, id)
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
		deposit, err := s.repo.GetInterbankDepositByID(ctx, tenantID, movement.DepositID)
		if err != nil {
			return err
		}
		if deposit == nil {
			return ardaerrors.New(ardaerrors.CodeNotFound, "interbank deposit not found")
		}
		if err := s.postIBM(ctx, deposit, movement, ibmCard(kind)); err != nil {
			return err
		}
		switch kind {
		case IBMKindTopUp:
			if err := s.repo.ApplyIBMTopUp(ctx, tenantID, deposit.ID, movement.AmountMinor); err != nil {
				return err
			}
		case IBMKindInterest:
			if err := s.repo.ApplyIBMAccrual(ctx, tenantID, deposit.ID, 0, movement.MovementDate); err != nil {
				return err
			}
		case IBMKindExpected:
			if err := s.repo.ApplyIBMAccrual(ctx, tenantID, deposit.ID, movement.AmountMinor, movement.MovementDate); err != nil {
				return err
			}
		case IBMKindWithdraw:
			if err := s.repo.ApplyIBMWithdraw(ctx, tenantID, deposit.ID, movement.AmountMinor); err != nil {
				return err
			}
		}
		return nil
	case "REJECT":
		if movement.Status == "REJECTED" {
			return nil
		}
		if movement.Status != "SUBMITTED" {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, "movement is not SUBMITTED")
		}
		return s.repo.SetIBMMovementStatus(ctx, tenantID, movement.ID, "REJECTED")
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "decision must be APPROVE or REJECT")
	}
}

// ── Internals ──

func ibmCard(kind string) string {
	switch kind {
	case IBMKindTopUp:
		return "IBM_TOP_UP"
	case IBMKindInterest:
		return "IBM_INTEREST"
	case IBMKindExpected:
		return "IBM_EXPECTED"
	case IBMKindWithdraw:
		return "IBM_WITHDRAW"
	default:
		return "IBM_TOP_UP"
	}
}

func ibmMovementTitle(kind string) string {
	switch kind {
	case IBMKindTopUp:
		return "Nộp thêm tiền gửi liên ngân hàng"
	case IBMKindInterest:
		return "Thu lãi tiền gửi liên ngân hàng"
	case IBMKindExpected:
		return "Dự thu lãi tiền gửi liên ngân hàng"
	case IBMKindWithdraw:
		return "Rút tiền gửi liên ngân hàng"
	default:
		return "Giao dịch tiền gửi liên ngân hàng"
	}
}

func ibmLegs(card string) (string, string) {
	switch card {
	case "IBM_PLACE":
		return "IBM_INTERBANK_DEPOSIT", "CASH_SETTLEMENT_ACCOUNT"
	case "IBM_TOP_UP":
		return "IBM_INTERBANK_DEPOSIT", "CASH_SETTLEMENT_ACCOUNT"
	case "IBM_INTEREST":
		return "CASH_SETTLEMENT_ACCOUNT", "IBM_INTEREST_INCOME"
	case "IBM_EXPECTED":
		return "IBM_INTEREST_RECEIVABLE", "IBM_INTEREST_INCOME"
	case "IBM_WITHDRAW":
		return "CASH_SETTLEMENT_ACCOUNT", "IBM_INTERBANK_DEPOSIT"
	default:
		return "IBM_INTERBANK_DEPOSIT", "CASH_SETTLEMENT_ACCOUNT"
	}
}

// postIBM posts one 2-leg entry for the IBM card. PLACE posts the contract
// principal; movements post their amount.
func (s *IBMService) postIBM(ctx context.Context, deposit *repository.InterbankDeposit, movement *repository.IBMMovement, card string) error {
	if s.finance == nil {
		return ardaerrors.New(ardaerrors.CodeInternal, "finance client is not configured")
	}
	amount := deposit.PrincipalMinor
	idempotencyKey := fmt.Sprintf("ibm-place-%s", deposit.ID)
	accountingDate := deposit.DepositDate
	description := fmt.Sprintf("Mở hợp đồng tiền gửi liên ngân hàng %s", deposit.DepositCode)
	documentID := deposit.ID
	if movement != nil {
		amount = movement.AmountMinor
		idempotencyKey = movement.IdempotencyKey
		accountingDate = movement.MovementDate
		description = fmt.Sprintf("%s %s", ibmMovementTitle(movement.Kind), deposit.DepositCode)
		documentID = movement.ID
	}
	debitClass, creditClass := ibmLegs(card)
	analytics := func(class string) *financev1.Analytics {
		return &financev1.Analytics{
			AccClassification: class,
			OrgUnitCode:       deposit.OrgCode,
			FundSourceCode:    deposit.ProductCode,
			Dimensions:        map[string]string{"deposit_code": deposit.DepositCode},
		}
	}
	posted, err := s.finance.Post(ctx, &financev1.PostingRequest{
		IdempotencyKey: idempotencyKey,
		AccountingDate: accountingDate,
		CurrencyCode:   deposit.CurrencyCode,
		Description:    description,
		BusinessReference: &financev1.BusinessReference{
			Domain:       "ibm",
			DocumentType: card,
			DocumentId:   documentID,
			DocumentCode: deposit.DepositCode,
		},
		Lines: []*financev1.PostingLine{
			{LineNo: 1, Direction: "DEBIT", AmountMinor: amount, CurrencyCode: deposit.CurrencyCode, Analytics: analytics(debitClass)},
			{LineNo: 2, Direction: "CREDIT", AmountMinor: amount, CurrencyCode: deposit.CurrencyCode, Analytics: analytics(creditClass)},
		},
	})
	if err != nil {
		return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "interbank posting failed", err)
	}
	if movement != nil {
		return s.repo.SetIBMMovementJournal(ctx, deposit.TenantID, movement.ID, "POSTED", posted.GetJournalEntryId())
	}
	return s.repo.SetInterbankDepositJournal(ctx, deposit.TenantID, deposit.ID, posted.GetJournalEntryId())
}
