package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// Fund flow (phụ lục phân phối thu chi — PLI_B/PLI_C). The maker picks a fund
// and an action; finance-service builds the balanced posting lines from the
// FUND_* class maps seeded in `fin_acc_class_coa_maps`:
//   - APPROPRIATION (trích lập): DEBIT FUND_PROFIT_SOURCE (4211), CREDIT FUND_<fund>.
//   - UTILIZATION (sử dụng):     DEBIT FUND_<fund>,                CREDIT FUND_CASH (1131).
//
// The case rides the standard two-phase lifecycle like the manual posting legs.
const (
	FlowFund = "FUND"

	CaseTypeFinFundApprop = "FIN_FUND_APPROP_V2"
	CaseTypeFinFundUse    = "FIN_FUND_USE_V2"

	FundActionAppropriation = "APPROPRIATION"
	FundActionUtilization   = "UTILIZATION"

	fundDocTypeApprop     = "FIN_FUND_APPROP"
	fundDocTypeUse        = "FIN_FUND_USE"
	fundCurrencyCode      = "VND"
	fundClassProfitSource = "FUND_PROFIT_SOURCE"
	fundClassCash         = "FUND_CASH"
)

// fundClassByCode maps the request fund code to its class-map classification.
var fundClassByCode = map[string]string{
	"DEV":           "FUND_DEV",
	"FIN_RESERVE":   "FUND_FIN_RESERVE",
	"OP_SUPPLEMENT": "FUND_OP_SUPPLEMENT",
	"BONUS_MANAGER": "FUND_BONUS_MANAGER",
	"BONUS_STAFF":   "FUND_BONUS_STAFF",
	"WELFARE_FIXED": "FUND_WELFARE_FIXED",
	"WELFARE_BOD":   "FUND_WELFARE_BOD",
}

// FundCaseInput is the FUND flow body (service shape; the HTTP handler maps
// the snake_case fund_request).
type FundCaseInput struct {
	AccountingDate string
	Action         string
	FundCode       string
	AmountMinor    int64
	Description    string
	Trader         *CancellationTrader
	IdempotencyKey string
}

// buildFundPostingRequest builds the balanced PostingRequest from the fund
// class maps; lines carry acc_classification (resolved by posting validation).
func buildFundPostingRequest(in *FundCaseInput) (*financev1.PostingRequest, string, string, error) {
	action := strings.ToUpper(strings.TrimSpace(in.Action))
	fundClass, ok := fundClassByCode[strings.ToUpper(strings.TrimSpace(in.FundCode))]
	if !ok {
		return nil, "", "", fmt.Errorf("fund_request.fund_code is invalid")
	}
	if in.AmountMinor <= 0 {
		return nil, "", "", fmt.Errorf("fund_request.amount_minor must be positive")
	}

	var lines []*financev1.PostingLine
	var docType, caseType string
	switch action {
	case FundActionAppropriation:
		docType, caseType = fundDocTypeApprop, CaseTypeFinFundApprop
		lines = []*financev1.PostingLine{
			{LineNo: 1, Direction: "DEBIT", AmountMinor: in.AmountMinor, CurrencyCode: fundCurrencyCode,
				Analytics: &financev1.Analytics{AccClassification: fundClassProfitSource}},
			{LineNo: 2, Direction: "CREDIT", AmountMinor: in.AmountMinor, CurrencyCode: fundCurrencyCode,
				Analytics: &financev1.Analytics{AccClassification: fundClass}},
		}
	case FundActionUtilization:
		docType, caseType = fundDocTypeUse, CaseTypeFinFundUse
		lines = []*financev1.PostingLine{
			{LineNo: 1, Direction: "DEBIT", AmountMinor: in.AmountMinor, CurrencyCode: fundCurrencyCode,
				Analytics: &financev1.Analytics{AccClassification: fundClass}},
			{LineNo: 2, Direction: "CREDIT", AmountMinor: in.AmountMinor, CurrencyCode: fundCurrencyCode,
				Analytics: &financev1.Analytics{AccClassification: fundClassCash}},
		}
	default:
		return nil, "", "", fmt.Errorf("fund_request.action must be APPROPRIATION or UTILIZATION")
	}

	description := strings.TrimSpace(in.Description)
	if description == "" {
		description = fmt.Sprintf("Quỹ %s — %s (%s)", in.FundCode, action, in.AccountingDate)
	}
	req := &financev1.PostingRequest{
		AccountingDate: in.AccountingDate,
		CurrencyCode:   fundCurrencyCode,
		Description:    description,
		Lines:          lines,
		BusinessReference: &financev1.BusinessReference{
			Domain:       "fin",
			DocumentType: docType,
		},
	}
	return req, docType, caseType, nil
}

// CreateFundCase builds and validates the fund posting, then creates and
// submits the FIN_FUND_APPROP_V2 / FIN_FUND_USE_V2 maker-checker case.
func (s *PostingCaseService) CreateFundCase(ctx context.Context, tenantID, actor string, in *FundCaseInput) (*PostingCaseResult, error) {
	if in == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "fund_request is required")
	}
	in.AccountingDate = strings.TrimSpace(in.AccountingDate)
	if _, err := time.Parse("2006-01-02", in.AccountingDate); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "fund_request.accounting_date must be YYYY-MM-DD")
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}

	req, docType, caseType, err := buildFundPostingRequest(in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	if err := s.posting.EnsurePostingDateAllowed(ctx, tenantID, docType, in.AccountingDate); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	if _, err := s.posting.ValidatePosting(ctx, tenantID, req); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}

	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("fin-fund-%s", newRandomUUID())
		in.IdempotencyKey = idempotencyKey
	}
	req.IdempotencyKey = idempotencyKey
	req.Metadata = map[string]string{"actor": actor}

	title := "Quỹ — " + truncateDescription(req.GetDescription())
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          caseType,
		CaseCode:          "",
		Title:             title,
		PrimaryObjectType: primaryObjectTypeFinPosting,
		PrimaryObjectID:   idempotencyKey,
		DomainService:     domainServiceFinance,
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    idempotencyKey,
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}

	variables := map[string]any{
		"flow":                  FlowFund,
		"postingIdempotencyKey": idempotencyKey,
		"postingRequest":        postingRequestVariables(req),
		"fundMeta": map[string]any{
			"action":      strings.ToUpper(strings.TrimSpace(in.Action)),
			"fundCode":    strings.ToUpper(strings.TrimSpace(in.FundCode)),
			"amountMinor": in.AmountMinor,
		},
	}
	if in.Trader != nil {
		trader := map[string]any{}
		if v := in.Trader.ObjectType; v != "" {
			trader["objectType"] = v
		}
		if v := in.Trader.ObjectCode; v != "" {
			trader["objectCode"] = v
		}
		if v := in.Trader.ObjectName; v != "" {
			trader["objectName"] = v
		}
		if len(trader) > 0 {
			variables["trader"] = trader
		}
	}
	if _, err := s.workflow.SubmitCase(ctx, caseCreated.GetId(), actor, variables, idempotencyKey+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	return &PostingCaseResult{
		CaseID:   caseCreated.GetId(),
		CaseCode: caseCreated.GetCaseCode(),
	}, nil
}
