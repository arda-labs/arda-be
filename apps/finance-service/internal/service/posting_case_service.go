package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// Manual posting flow case types (arda iteration 9). The FE posts
// accountant-picked lines (account_code on every line, no classification);
// the case rides the standard finance two-phase posting lifecycle:
// Reserve at init → Validate at validate → Post on approve → Release on reject.
const (
	FlowSingleEntry = "SINGLE_ENTRY"
	FlowDoubleEntry = "DOUBLE_ENTRY"

	CaseTypeFinSingleEntry = "FIN_SINGLE_ENTRY_V2"
	CaseTypeFinDoubleEntry = "FIN_DOUBLE_ENTRY_V2"

	primaryObjectTypeFinPosting = "fin.posting"
	domainServiceFinance        = "finance-service"
)

// WorkflowCaseClient is the workflow surface CreatePostingCase needs —
// mirrored on workflowclient.Client so tests can fake it.
type WorkflowCaseClient interface {
	CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error)
	SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error)
}

// PostingCaseService creates manual-posting workflow cases on top of the
// PostingService two-phase lifecycle.
type PostingCaseService struct {
	posting  *PostingService
	workflow WorkflowCaseClient
}

func NewPostingCaseService(posting *PostingService, workflow WorkflowCaseClient) *PostingCaseService {
	return &PostingCaseService{posting: posting, workflow: workflow}
}

// PostingCaseInput is the HTTP contract body of POST /api/finance/posting-cases.
type PostingCaseInput struct {
	Flow           string
	PostingRequest *financev1.PostingRequest
}

// PostingCaseResult is the HTTP contract response: the created workflow case.
type PostingCaseResult struct {
	CaseID   string `json:"case_id"`
	CaseCode string `json:"case_code"`
}

// validateManualPostingFlow is the structural pre-check for manual posting
// flows, before any resolvable-account work happens. SINGLE_ENTRY is the
// two-line "bút toán lẻ" pair (one DEBIT + one CREDIT, equal amounts);
// DOUBLE_ENTRY ("bút toán kép") accepts any ≥2-line entry balanced per
// currency — the same ΣDEBIT=ΣCREDIT rule PostingService enforces later.
func validateManualPostingFlow(flow string, req *financev1.PostingRequest) error {
	if flow != FlowSingleEntry && flow != FlowDoubleEntry {
		return fmt.Errorf("flow must be SINGLE_ENTRY or DOUBLE_ENTRY")
	}
	if req == nil {
		return fmt.Errorf("posting_request is required")
	}
	if strings.TrimSpace(req.GetAccountingDate()) == "" {
		return fmt.Errorf("posting_request.accounting_date is required")
	}
	if _, err := time.Parse("2006-01-02", req.GetAccountingDate()); err != nil {
		return fmt.Errorf("posting_request.accounting_date must be YYYY-MM-DD")
	}
	if len(req.GetLines()) == 0 {
		return fmt.Errorf("posting_request.lines must not be empty")
	}
	for _, l := range req.GetLines() {
		if strings.TrimSpace(l.GetAccountCode()) == "" {
			return fmt.Errorf("posting_request.lines[%d].account_code is required for manual posting", l.GetLineNo())
		}
	}
	switch flow {
	case FlowSingleEntry:
		return validateSingleEntryShape(req.GetLines())
	default:
		return validateDoubleEntryShape(req.GetLines())
	}
}

// validateSingleEntryShape: exactly 2 lines, one DEBIT + one CREDIT, equal
// amounts (per the FE contract for bút toán lẻ).
func validateSingleEntryShape(lines []*financev1.PostingLine) error {
	if len(lines) != 2 {
		return fmt.Errorf("SINGLE_ENTRY requires exactly 2 lines, got %d", len(lines))
	}
	debit, credit := lines[0], lines[1]
	if debit.GetDirection() == "CREDIT" && credit.GetDirection() == "DEBIT" {
		debit, credit = credit, debit
	}
	if debit.GetDirection() != "DEBIT" {
		return fmt.Errorf("SINGLE_ENTRY requires exactly one DEBIT line, got %q and %q", lines[0].GetDirection(), lines[1].GetDirection())
	}
	if credit.GetDirection() != "CREDIT" {
		return fmt.Errorf("SINGLE_ENTRY requires exactly one CREDIT line, got %q and %q", lines[0].GetDirection(), lines[1].GetDirection())
	}
	if debit.GetAmountMinor() != credit.GetAmountMinor() {
		return fmt.Errorf("SINGLE_ENTRY requires equal debit and credit amounts (%d ≠ %d)",
			debit.GetAmountMinor(), credit.GetAmountMinor())
	}
	return nil
}

// validateDoubleEntryShape: at least 2 lines, ΣDEBIT = ΣCREDIT per currency
// (bút toán kép). The mirror of the balance check PostingService applies at
// resolve time, surfaced here so the FE gets a clean error before the case
// exists.
func validateDoubleEntryShape(lines []*financev1.PostingLine) error {
	if len(lines) < 2 {
		return fmt.Errorf("DOUBLE_ENTRY requires at least 2 lines, got %d", len(lines))
	}
	balanced := map[string][2]int64{}
	for _, l := range lines {
		currency := l.GetCurrencyCode()
		if currency == "" {
			currency = "VND"
		}
		sums := balanced[currency]
		switch l.GetDirection() {
		case "DEBIT":
			sums[0] += l.GetAmountMinor()
		case "CREDIT":
			sums[1] += l.GetAmountMinor()
		}
		balanced[currency] = sums
	}
	for currency, sums := range balanced {
		if sums[0] != sums[1] {
			return fmt.Errorf("DOUBLE_ENTRY is unbalanced for %s: debit %d ≠ credit %d", currency, sums[0], sums[1])
		}
	}
	return nil
}

// CreatePostingCase validates the posting structurally, previews it through
// PostingService.ValidatePosting (COA resolution + balance rules), then
// creates and submits the maker-checker workflow case. The case idempotency
// key is generated BEFORE CreateCase so a retry after a partial failure
// replays instead of opening a second case.
func (s *PostingCaseService) CreatePostingCase(ctx context.Context, tenantID, actor string, in PostingCaseInput) (*PostingCaseResult, error) {
	if err := validateManualPostingFlow(in.Flow, in.PostingRequest); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	if _, err := s.posting.ValidatePosting(ctx, tenantID, in.PostingRequest); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}

	flowLower := strings.ToLower(in.Flow)
	// The FE may pin the idempotency key (retry-safe replay of the whole
	// create+submit); otherwise the server generates one BEFORE CreateCase so
	// a partial failure never opens a second case for one attempt.
	idempotencyKey := in.PostingRequest.GetIdempotencyKey()
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("fin-%s-%s", flowLower, newRandomUUID())
		in.PostingRequest.IdempotencyKey = idempotencyKey
	}
	caseType := CaseTypeFinSingleEntry
	title := "Bút toán lẻ — "
	if in.Flow == FlowDoubleEntry {
		caseType = CaseTypeFinDoubleEntry
		title = "Bút toán kép — "
	}
	title += truncateDescription(in.PostingRequest.GetDescription())

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

	if _, err := s.workflow.SubmitCase(ctx, caseCreated.GetId(), actor, map[string]any{
		"flow":                  in.Flow,
		"postingIdempotencyKey": idempotencyKey,
		"postingRequest":        postingRequestVariables(in.PostingRequest),
	}, idempotencyKey+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	return &PostingCaseResult{
		CaseID:   caseCreated.GetId(),
		CaseCode: caseCreated.GetCaseCode(),
	}, nil
}

// postingRequestVariables serializes the posting request into the camelCase
// JSON object the manual posting workers deserialize (map[string]any keeps
// structpb happy in SubmitCase).
func postingRequestVariables(req *financev1.PostingRequest) map[string]any {
	lines := make([]any, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		line := map[string]any{
			"lineNo":       l.GetLineNo(),
			"direction":    l.GetDirection(),
			"amountMinor":  l.GetAmountMinor(),
			"accountCode":  l.GetAccountCode(),
		}
		if v := l.GetCurrencyCode(); v != "" {
			line["currencyCode"] = v
		}
		if v := l.GetCoaVersion(); v != "" {
			line["coaVersion"] = v
		}
		if v := l.GetCounterpartyCode(); v != "" {
			line["counterpartyCode"] = v
		}
		if v := l.GetDescription(); v != "" {
			line["description"] = v
		}
		lines = append(lines, line)
	}
	vars := map[string]any{
		"accountingDate": req.GetAccountingDate(),
		"currencyCode":   req.GetCurrencyCode(),
		"description":    req.GetDescription(),
		"lines":          lines,
	}
	if v := req.GetIdempotencyKey(); v != "" {
		vars["idempotencyKey"] = v
	}
	return vars
}

func truncateDescription(description string) string {
	description = strings.TrimSpace(description)
	runes := []rune(description)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return description
}

// newRandomUUID is a panic-free RFC 4122 v4 id used for case idempotency.
func newRandomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the runtime is broken; a plain hex fallback
		// still keeps the request unique-enough for the idempotency key shape.
		return fmt.Sprintf("%x-%x", time.Now().UnixNano(), b[:])
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
