package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

const (
	ProductRequestRegister = "REGISTER"
	ProductRequestEdit     = "EDIT"

	productRequestRegisterCaseType = "DPM_PRODUCT_REGISTER_V1"
	productRequestEditCaseType     = "DPM_PRODUCT_EDIT_V1"
)

var depositCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ProductRequestService runs the DPM.102/103 product register/edit approval
// flows: the maker stages the payload, the checker approval applies it to
// dpm_products (nothing is written before that).
type ProductRequestService struct {
	repo     *repository.DepositRepository
	workflow WorkflowSubmitter
}

func NewProductRequestService(repo *repository.DepositRepository, workflow WorkflowSubmitter) *ProductRequestService {
	return &ProductRequestService{repo: repo, workflow: workflow}
}

// ProductRequestInput is the HTTP contract body.
type ProductRequestInput struct {
	RequestType  string  `json:"request_type"`
	ProductCode  string  `json:"product_code"`
	Name         string  `json:"name"`
	TermMonths   int     `json:"term_months"`
	InterestRate float64 `json:"interest_rate"`
	CurrencyCode string  `json:"currency_code"`
}

// Submit validates the payload, stages the request and starts the
// maker/checker case.
func (s *ProductRequestService) Submit(ctx context.Context, tenantID, actor string, in ProductRequestInput) (*Submission, error) {
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	requestType := strings.ToUpper(strings.TrimSpace(in.RequestType))
	in.RequestType = requestType
	code := strings.TrimSpace(in.ProductCode)
	name := strings.TrimSpace(in.Name)
	switch {
	case requestType != ProductRequestRegister && requestType != ProductRequestEdit:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "request_type must be REGISTER or EDIT")
	case !depositCodePattern.MatchString(code):
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "product_code is invalid")
	case name == "":
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "name is required")
	case in.TermMonths <= 0:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "term_months must be positive")
	case in.InterestRate < 0:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "interest_rate must not be negative")
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	exists, err := s.productExists(ctx, tenantID, code)
	if err != nil {
		return nil, err
	}
	if requestType == ProductRequestRegister && exists {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, "product code already exists")
	}
	if requestType == ProductRequestEdit && !exists {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "product not found: "+code)
	}

	request, err := s.repo.InsertProductRequest(ctx, &repository.ProductRequest{
		TenantID:     tenantID,
		RequestType:  requestType,
		ProductCode:  code,
		Name:         name,
		TermMonths:   in.TermMonths,
		InterestRate: in.InterestRate,
		CurrencyCode: in.CurrencyCode,
		CreatedBy:    actor,
	})
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	caseType := productRequestRegisterCaseType
	action := "Đăng ký"
	if requestType == ProductRequestEdit {
		caseType = productRequestEditCaseType
		action = "Điều chỉnh"
	}
	key := idempotencyKey("dpm-product-"+strings.ToLower(requestType), code)
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          caseType,
		Title:             action + " sản phẩm " + code,
		PrimaryObjectType: "dpm.product_request",
		PrimaryObjectID:   request.ID,
		DomainService:     "deposit-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    key,
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	if _, err := s.workflow.SubmitCase(ctx, caseCreated.GetId(), actor,
		map[string]any{"productRequestId": request.ID}, key+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetProductRequestCase(ctx, tenantID, request.ID, caseCreated.GetId(), caseCreated.GetCaseCode()); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return &Submission{CaseID: caseCreated.GetId(), CaseCode: caseCreated.GetCaseCode()}, nil
}

// Check validates the request is actionable (BPMN validate job).
func (s *ProductRequestService) Check(ctx context.Context, tenantID, id string) (bool, string, error) {
	request, err := s.repo.GetProductRequest(ctx, tenantID, id)
	if err != nil {
		return false, "product request not found: " + id, nil
	}
	if request.Status != "SUBMITTED" {
		return false, "status " + request.Status + " is not actionable", nil
	}
	return true, "ok", nil
}

// Resolve applies the checker decision: APPROVE upserts dpm_products and
// marks the request APPLIED; anything else marks it REJECTED.
func (s *ProductRequestService) Resolve(ctx context.Context, tenantID, id, decision, actor string) error {
	request, err := s.repo.GetProductRequest(ctx, tenantID, id)
	if err != nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "product request not found: "+id)
	}
	if request.Status != "SUBMITTED" {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "product request is not awaiting approval")
	}
	if decision != "APPROVE" {
		if err := s.repo.MarkProductRequestResolved(ctx, tenantID, id, "REJECTED"); err != nil {
			return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		return nil
	}
	if _, err := s.repo.UpsertProduct(ctx, &repository.SavingsProduct{
		TenantID:     tenantID,
		Code:         request.ProductCode,
		Name:         request.Name,
		TermMonths:   request.TermMonths,
		InterestRate: request.InterestRate,
		CurrencyCode: request.CurrencyCode,
		CreatedBy:    actor,
	}); err != nil {
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if err := s.repo.MarkProductRequestResolved(ctx, tenantID, id, "APPLIED"); err != nil {
		return ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return nil
}

// List returns staged requests (optional status filter).
func (s *ProductRequestService) List(ctx context.Context, tenantID, status string) ([]repository.ProductRequest, error) {
	items, err := s.repo.ListProductRequests(ctx, tenantID, status)
	return items, mapErr(err)
}

func (s *ProductRequestService) productExists(ctx context.Context, tenantID, code string) (bool, error) {
	products, err := s.repo.ListProducts(ctx, repository.ListProductsParams{TenantID: tenantID})
	if err != nil {
		return false, mapErr(err)
	}
	for _, p := range products {
		if p.Code == code {
			return true, nil
		}
	}
	return false, nil
}
