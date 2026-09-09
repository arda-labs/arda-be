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

// Closing flow (arda iteration 11 — kết chuyển thu chi, FAC.203.01). The
// maker picks INC/EXP accounts with closing amounts (prefilled from the
// candidate balances); finance-service builds the balanced posting lines
// server-side: every INC row → DEBIT the income account, every EXP row →
// CREDIT the expense account, then Σ(INC) → one CREDIT on the
// FIN_CLOSING_INC_DEST account and Σ(EXP) → one DEBIT on the
// FIN_CLOSING_EXP_DEST account. The case rides the same two-phase lifecycle
// as the manual posting flows (Reserve at init → Validate → Post on approve
// → Release on reject), carried by the fin-closing-v2.bpmn process.
const (
	FlowClosing       = "CLOSING"
	CaseTypeFinClosing = "FIN_CLOSING_V2"

	// ClosingPeriodTypes is the whitelist of closing period granularities
	// (D|M|Q|Y); an empty request defaults to Y (kỳ năm).
	closingPeriodTypes = "DMQY"

	closingCurrencyCode = "VND"
	closingDocType      = "FIN_CLOSING"
)

// ClosingCaseRow is one maker-picked closing line: the INC/EXP account and
// the amount to close (the FE prefills it with the account's natural
// balance).
type ClosingCaseRow struct {
	AccCode     string
	AccPurpose  string
	AmountMinor int64
}

// ClosingCaseInput is the CLOSING flow body (mirrors the cancellation input:
// snake_case JSON handled by the HTTP handler, this is the service shape).
type ClosingCaseInput struct {
	AccountingDate string
	PeriodType     string
	Description    string
	Trader         *CancellationTrader
	IdempotencyKey string
	Rows           []ClosingCaseRow
}

// closingTotals summarizes the built closing: Σ(INC), Σ(EXP) and the period
// business result (income − expense) carried into the case variables as
// closingMeta.
type closingTotals struct {
	PeriodType        string
	TotalIncomeMinor  int64
	TotalExpenseMinor int64
}

func (t closingTotals) businessResultMinor() int64 {
	return t.TotalIncomeMinor - t.TotalExpenseMinor
}

// validateClosingShape is the structural pre-check for the closing request,
// before any resolvable-account work happens.
func validateClosingShape(in *ClosingCaseInput) error {
	if in == nil {
		return fmt.Errorf("closing_request is required")
	}
	if strings.TrimSpace(in.AccountingDate) == "" {
		return fmt.Errorf("closing_request.accounting_date is required")
	}
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(in.AccountingDate)); err != nil {
		return fmt.Errorf("closing_request.accounting_date must be YYYY-MM-DD")
	}
	if in.PeriodType == "" {
		in.PeriodType = "Y"
	}
	if !strings.Contains(closingPeriodTypes, in.PeriodType) || len(in.PeriodType) != 1 {
		return fmt.Errorf("closing_request.period_type must be one of D, M, Q, Y")
	}
	if len(in.Rows) == 0 {
		return fmt.Errorf("closing_request.rows must not be empty")
	}
	for i, row := range in.Rows {
		if strings.TrimSpace(row.AccCode) == "" {
			return fmt.Errorf("closing_request.rows[%d].acc_code is required", i)
		}
		switch row.AccPurpose {
		case "INC", "EXP":
		default:
			return fmt.Errorf("closing_request.rows[%d].acc_purpose must be INC or EXP", i)
		}
		if row.AmountMinor <= 0 {
			return fmt.Errorf("closing_request.rows[%d].amount_minor must be positive", i)
		}
	}
	return nil
}

// buildClosingPostingRequest validates every row against the COA (account
// exists, is postable and carries the purpose the FE picked), resolves the
// closing destinations and builds the balanced PostingRequest the two-phase
// lifecycle rides on. Line order puts the destination CREDIT (income close)
// before the destination DEBIT (expense close) so a same-entry profit net
// covers its own availability at Reserve.
func (s *PostingCaseService) buildClosingPostingRequest(ctx context.Context, tenantID string, in *ClosingCaseInput) (*financev1.PostingRequest, closingTotals, error) {
	lines := make([]*financev1.PostingLine, 0, len(in.Rows)+2)
	var totals closingTotals
	totals.PeriodType = in.PeriodType

	lineNo := int32(0)
	nextLineNo := func() int32 {
		lineNo++
		return lineNo
	}

	for i, row := range in.Rows {
		acc, err := s.posting.ResolveClosingAccount(ctx, tenantID, strings.TrimSpace(row.AccCode), in.AccountingDate)
		if err != nil {
			return nil, totals, fmt.Errorf("closing_request.rows[%d]: %s", i, err.Error())
		}
		if acc.AccPurpose != row.AccPurpose {
			return nil, totals, fmt.Errorf(
				"closing_request.rows[%d]: account %s carries acc_purpose %q, request says %q",
				i, row.AccCode, purposeLabel(acc.AccPurpose), row.AccPurpose)
		}
		direction := "DEBIT"
		if row.AccPurpose == "EXP" {
			direction = "CREDIT"
		}
		lines = append(lines, &financev1.PostingLine{
			LineNo:       nextLineNo(),
			Direction:    direction,
			AmountMinor:  row.AmountMinor,
			CurrencyCode: closingCurrencyCode,
			AccountCode:  acc.AccountCode,
			CoaVersion:   acc.CoaVersion,
		})
		if row.AccPurpose == "INC" {
			totals.TotalIncomeMinor += row.AmountMinor
		} else {
			totals.TotalExpenseMinor += row.AmountMinor
		}
	}

	// Destination lines: only the sides that actually carry amounts need a
	// destination; a missing rule for a needed side is a config error the
	// maker cannot fix — INVALID_INPUT with the rule key named.
	if totals.TotalIncomeMinor > 0 {
		dest, err := s.posting.ClosingDest(ctx, tenantID, "INC")
		if err != nil {
			return nil, totals, err
		}
		acc, err := s.posting.ResolveClosingAccount(ctx, tenantID, dest, in.AccountingDate)
		if err != nil {
			return nil, totals, fmt.Errorf("closing destination INC: %s", err.Error())
		}
		lines = append(lines, &financev1.PostingLine{
			LineNo:       nextLineNo(),
			Direction:    "CREDIT",
			AmountMinor:  totals.TotalIncomeMinor,
			CurrencyCode: closingCurrencyCode,
			AccountCode:  acc.AccountCode,
			CoaVersion:   acc.CoaVersion,
		})
	}
	if totals.TotalExpenseMinor > 0 {
		dest, err := s.posting.ClosingDest(ctx, tenantID, "EXP")
		if err != nil {
			return nil, totals, err
		}
		acc, err := s.posting.ResolveClosingAccount(ctx, tenantID, dest, in.AccountingDate)
		if err != nil {
			return nil, totals, fmt.Errorf("closing destination EXP: %s", err.Error())
		}
		lines = append(lines, &financev1.PostingLine{
			LineNo:       nextLineNo(),
			Direction:    "DEBIT",
			AmountMinor:  totals.TotalExpenseMinor,
			CurrencyCode: closingCurrencyCode,
			AccountCode:  acc.AccountCode,
			CoaVersion:   acc.CoaVersion,
		})
	}

	description := strings.TrimSpace(in.Description)
	if description == "" {
		description = fmt.Sprintf("Kết chuyển thu chi kỳ %s — %s", totals.PeriodType, in.AccountingDate)
	}

	req := &financev1.PostingRequest{
		AccountingDate: in.AccountingDate,
		CurrencyCode:   closingCurrencyCode,
		Description:    description,
		Lines:          lines,
		BusinessReference: &financev1.BusinessReference{
			Domain:       "fin",
			DocumentType: closingDocType,
		},
	}
	return req, totals, nil
}

func purposeLabel(purpose string) string {
	if purpose == "" {
		return "NULL"
	}
	return purpose
}

// CreateClosingCase validates the closing structurally and against the COA,
// builds the posting lines server-side, then creates and submits the
// FIN_CLOSING_V2 maker-checker case with the posting riding the two-phase
// lifecycle. The posting-date policy runs here (fail fast on submit) and
// again at Reserve inside the workflow init worker.
func (s *PostingCaseService) CreateClosingCase(ctx context.Context, tenantID, actor string, in *ClosingCaseInput) (*PostingCaseResult, error) {
	if err := validateClosingShape(in); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	// Fail fast on the posting-date policy so the maker fixes the date at
	// submit time instead of after case creation.
	if err := s.posting.EnsurePostingDateAllowed(ctx, tenantID, closingDocType, in.AccountingDate); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}

	req, totals, err := s.buildClosingPostingRequest(ctx, tenantID, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	// Preview through the standard validation (period open, ΣD=ΣC, account
	// resolution) — the same pre-check the manual posting flows run.
	if _, err := s.posting.ValidatePosting(ctx, tenantID, req); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}

	// The FE may pin the idempotency key (retry-safe replay of the whole
	// create+submit); otherwise the server generates one BEFORE CreateCase so
	// a partial failure never opens a second case for one attempt.
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("fin-closing-%s", newRandomUUID())
		in.IdempotencyKey = idempotencyKey
	}
	req.IdempotencyKey = idempotencyKey

	title := "Kết chuyển thu chi — " + truncateDescription(req.GetDescription())

	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseTypeFinClosing,
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

	if _, err := s.workflow.SubmitCase(ctx, caseCreated.GetId(), actor, map[string]any{
		"flow":                  FlowClosing,
		"postingIdempotencyKey": idempotencyKey,
		"postingRequest":        postingRequestVariables(req),
		"closingMeta":           closingMetaVariables(in, totals),
	}, idempotencyKey+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	return &PostingCaseResult{
		CaseID:   caseCreated.GetId(),
		CaseCode: caseCreated.GetCaseCode(),
	}, nil
}

// ListClosingCandidates lists the INC/EXP accounts with a positive natural
// balance as of onDate (default today) — the closing candidate picker read
// model. Delegates to the PostingService read surface.
func (s *PostingCaseService) ListClosingCandidates(ctx context.Context, tenantID, onDate string) ([]ClosingCandidateRow, error) {
	return s.posting.ClosingCandidates(ctx, tenantID, onDate)
}

// closingMetaVariables serializes the closing summary the FE shows on the
// case detail: period granularity, Σ income / Σ expense and the period
// business result, plus the optional trader block (mirror of the
// cancellation trader).
func closingMetaVariables(in *ClosingCaseInput, totals closingTotals) map[string]any {
	meta := map[string]any{
		"periodType":           totals.PeriodType,
		"businessResultMinor":  totals.businessResultMinor(),
		"totalIncomeMinor":     totals.TotalIncomeMinor,
		"totalExpenseMinor":    totals.TotalExpenseMinor,
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
		if v := in.Trader.IDNumber; v != "" {
			trader["idNumber"] = v
		}
		if v := in.Trader.IssueDate; v != "" {
			trader["issueDate"] = v
		}
		if v := in.Trader.IssuePlace; v != "" {
			trader["issuePlace"] = v
		}
		if v := in.Trader.Address; v != "" {
			trader["address"] = v
		}
		meta["trader"] = trader
	}
	return meta
}
