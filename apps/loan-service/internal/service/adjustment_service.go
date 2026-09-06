package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	loangrpc "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// AdjustmentSubmitter is the workflow submission contract (workflow client).
type AdjustmentSubmitter interface {
	CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error)
	SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error)
}

func mapRepoError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case strings.Contains(err.Error(), "lnm: record not found"):
		return ardaerrors.New(ardaerrors.CodeNotFound, "loan record not found")
	case strings.Contains(err.Error(), "lnm: code conflict"):
		return ardaerrors.New(ardaerrors.CodeConflict, "loan code already exists")
	default:
		return ardaerrors.Wrap(ardaerrors.CodeInternal, "loan operation failed", err)
	}
}

// ── Adjustment flows (uniform) ──

// AdjustmentService runs the ten loan adjustment flows through workflow cases.
type AdjustmentService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewAdjustmentService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *AdjustmentService {
	return &AdjustmentService{repo: repo, workflow: workflow}
}

func CaseTypeForKind(kind string) string {
	upper := strings.ToUpper(strings.ReplaceAll(kind, "-", "_"))
	return "LNM_" + upper + "_V2"
}

func (s *AdjustmentService) List(ctx context.Context, kind, tenantID, contractCode, status string) ([]domain.Adjustment, error) {
	table, err := resolveKind(kind)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListAdjustments(ctx, table, tenantID, contractCode, status)
	return items, mapRepoError(err)
}

func (s *AdjustmentService) Get(ctx context.Context, kind, tenantID, id string) (domain.Adjustment, error) {
	table, err := resolveKind(kind)
	if err != nil {
		return domain.Adjustment{}, err
	}
	item, err := s.repo.GetAdjustment(ctx, table, tenantID, id)
	if err = mapRepoError(err); err != nil {
		return domain.Adjustment{}, err
	}
	return item, nil
}

// Create registers a DRAFT adjustment; flow-specific payload requirements are
// validated per kind here.
func (s *AdjustmentService) Create(ctx context.Context, kind, tenantID, createdBy string, in *domain.Adjustment) (domain.Adjustment, error) {
	table, err := resolveKind(kind)
	if err != nil {
		return domain.Adjustment{}, err
	}
	if strings.TrimSpace(in.ContractCode) == "" || !codePattern.MatchString(in.ContractCode) {
		return domain.Adjustment{}, ardaerrors.New(ardaerrors.CodeRequired, "contract_code is required")
	}
	if err := validateKindPayload(kind, in); err != nil {
		return domain.Adjustment{}, err
	}
	in.ID = repository.NewID(kindShortPrefix(kind))
	in.TenantID = tenantID
	in.Status = domain.AdjustmentDraft
	in.CreatedBy = createdBy
	item, err := s.repo.CreateAdjustment(ctx, table, in)
	return *item, mapRepoError(err)
}

// Submit pushes the DRAFT adjustment into a workflow case (PENDING).
func (s *AdjustmentService) Submit(ctx context.Context, kind, tenantID, actor, id string) (domain.Adjustment, error) {
	table, err := resolveKind(kind)
	if err != nil {
		return domain.Adjustment{}, err
	}
	if s.workflow == nil {
		return domain.Adjustment{}, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	item, err := s.repo.GetAdjustment(ctx, table, tenantID, id)
	if err = mapRepoError(err); err != nil {
		return domain.Adjustment{}, err
	}
	if item.Status != domain.AdjustmentDraft {
		return domain.Adjustment{}, ardaerrors.New(ardaerrors.CodeInvalidInput, "only DRAFT adjustments can be submitted")
	}
	title := kindTitle(kind) + " — " + item.ContractCode
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseTypeForKind(kind),
		CaseCode:          "",
		Title:             title,
		PrimaryObjectType: "lnm.adjustment." + kind,
		PrimaryObjectID:   item.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("lnm-%s-%s", kind, item.ID),
	})
	if err != nil {
		return domain.Adjustment{}, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"kind":          kind,
		"adjustmentId":  item.ID,
		"contractCode":  item.ContractCode,
		"effectiveDate": derefString(item.EffectiveDate),
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, fmt.Sprintf("lnm-%s-%s-submit", kind, item.ID)); err != nil {
		return domain.Adjustment{}, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetAdjustmentWorkflowCase(ctx, table, tenantID, item.ID, caseCreated.Id); err != nil {
		return domain.Adjustment{}, mapRepoError(err)
	}
	return s.Get(ctx, kind, tenantID, id)
}

// Resolve applies the workflow decision coming from the lnm.* workers.
func (s *AdjustmentService) Resolve(ctx context.Context, kind, tenantID, id, decision, decidedBy, note string) error {
	table, err := resolveKind(kind)
	if err != nil {
		return err
	}
	if _, err := s.repo.ResolveAdjustment(ctx, table, tenantID, id, decision, decidedBy, note); err != nil {
		return mapRepoError(err)
	}
	return nil
}

// Check validates the adjustment is in a state the flow can act on
// (validate job of the BPMN).
func (s *AdjustmentService) Check(ctx context.Context, kind, tenantID, id string) (bool, string, error) {
	item, err := s.Get(ctx, kind, tenantID, id)
	if err != nil {
		return false, err.Error(), nil
	}
	if item.Status != domain.AdjustmentPending {
		return false, fmt.Sprintf("adjustment status is %s, want PENDING", item.Status), nil
	}
	return true, "ok", nil
}

func resolveKind(kind string) (string, error) {
	if !loangrpc.IsValidKind(kind) {
		return "", ardaerrors.New(ardaerrors.CodeNotFound, fmt.Sprintf("unknown adjustment kind %q", kind))
	}
	table, ok := repository.AdjustmentTables[kind]
	if !ok {
		return "", ardaerrors.New(ardaerrors.CodeInternal, "kind table missing: "+kind)
	}
	return table, nil
}

// validateKindPayload enforces the per-flow payload contract.
func validateKindPayload(kind string, in *domain.Adjustment) error {
	payload := map[string]any{}
	if len(in.Payload) > 0 {
		if err := json.Unmarshal(in.Payload, &payload); err != nil {
			return ardaerrors.New(ardaerrors.CodeInvalidJSON, "payload must be a JSON object")
		}
	}
	has := func(key string) bool {
		_, ok := payload[key]
		return ok && payload[key] != nil
	}
	switch kind {
	case "debt-change":
		if !has("to_debt_group_code") {
			return ardaerrors.New(ardaerrors.CodeRequired, "payload.to_debt_group_code is required")
		}
	case "rate-change":
		if !has("new_rate") {
			return ardaerrors.New(ardaerrors.CodeRequired, "payload.new_rate is required")
		}
	case "restructure":
		if !has("new_term") || !has("new_maturity_date") {
			return ardaerrors.New(ardaerrors.CodeRequired, "payload.new_term and payload.new_maturity_date are required")
		}
	case "waiver":
		if in.Amount == nil && !has("waiver_percent") {
			return ardaerrors.New(ardaerrors.CodeRequired, "amount or payload.waiver_percent is required")
		}
	case "writeoff":
		if in.Amount == nil || !has("reason") {
			return ardaerrors.New(ardaerrors.CodeRequired, "amount and payload.reason are required")
		}
	case "recovery":
		if in.Amount == nil || in.AgreementCode == nil {
			return ardaerrors.New(ardaerrors.CodeRequired, "amount and agreement_code are required")
		}
	case "fund-check":
		if !has("result") {
			return ardaerrors.New(ardaerrors.CodeRequired, "payload.result is required")
		}
	case "revenue-allocation", "vfu-fee-allocation":
		if in.Amount == nil {
			return ardaerrors.New(ardaerrors.CodeRequired, "amount is required")
		}
	case "off-balance-export":
		if !has("reason") {
			return ardaerrors.New(ardaerrors.CodeRequired, "payload.reason is required")
		}
	}
	return nil
}

func kindShortPrefix(kind string) string {
	parts := strings.SplitN(kind, "-", 2)
	return "lnm" + parts[0]
}

func kindTitle(kind string) string {
	titles := map[string]string{
		"debt-change":        "Chuyển nhóm nợ",
		"rate-change":        "Thay đổi lãi suất",
		"restructure":        "Gia hạn nợ",
		"waiver":             "Miễn giảm lãi",
		"writeoff":           "Xử lý nợ",
		"recovery":           "Thu hồi nợ",
		"fund-check":         "Kiểm tra sử dụng vốn",
		"revenue-allocation": "Phân bổ doanh thu",
		"vfu-fee-allocation": "Trích phí ủy thác",
		"off-balance-export": "Xuất toán ngoại bảng",
	}
	if t, ok := titles[kind]; ok {
		return t
	}
	return kind
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
