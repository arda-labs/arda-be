package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
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
// Iteration 10 adds OFF_BALANCE (nhập/xuất ngoại bảng — same-shape lines on
// nature-B accounts) and CANCELLATION (hủy giao dịch — reverse a POSTED
// entry on checker approval; no posting_request, the original is referenced).
// Iteration 11 adds CLOSING (kết chuyển thu chi — server-built lines from
// INC/EXP rows, see closing_case_service.go).
const (
	FlowSingleEntry  = "SINGLE_ENTRY"
	FlowDoubleEntry  = "DOUBLE_ENTRY"
	FlowOffBalance   = "OFF_BALANCE"
	FlowCancellation = "CANCELLATION"

	CaseTypeFinSingleEntry  = "FIN_SINGLE_ENTRY_V2"
	CaseTypeFinDoubleEntry  = "FIN_DOUBLE_ENTRY_V2"
	CaseTypeFinOffBalance   = "FIN_OFF_BALANCE_V2"
	CaseTypeFinCancellation = "FIN_TXN_CANCEL_V2"

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
// PostingRequest backs SINGLE_ENTRY / DOUBLE_ENTRY / OFF_BALANCE; Cancellation
// backs CANCELLATION (no posting request — the original entry is referenced);
// Closing backs CLOSING (iteration 11 — server-built lines from INC/EXP rows).
type PostingCaseInput struct {
	Flow           string
	PostingRequest *financev1.PostingRequest
	Cancellation   *CancellationCaseInput
	Closing        *ClosingCaseInput
	Fund           *FundCaseInput
}

// CancellationTrader is the person raising the cancellation (mirror of the
// trader block the FE already sends for manual postings).
type CancellationTrader struct {
	ObjectType string
	ObjectCode string
	ObjectName string
	IDNumber   string
	IssueDate  string
	IssuePlace string
	Address    string
}

// CancellationCaseInput is the CANCELLATION flow body: which POSTED journal
// entry to reverse (by human entry_no) and why. AccountingDate is the
// reversal business date — empty defaults to the original entry's date at
// execute time.
type CancellationCaseInput struct {
	ReferenceEntryNo string
	Reason           string
	AccountingDate   string
	Trader           *CancellationTrader
	IdempotencyKey   string
}

// PostingCaseResult is the HTTP contract response: the created workflow case.
type PostingCaseResult struct {
	CaseID   string `json:"case_id"`
	CaseCode string `json:"case_code"`
}

// validateManualPostingFlow is the structural pre-check for the line-carrying
// manual posting flows, before any resolvable-account work happens.
// SINGLE_ENTRY is the two-line "bút toán lẻ" pair (one DEBIT + one CREDIT,
// equal amounts); DOUBLE_ENTRY ("bút toán kép") accepts any ≥2-line entry
// balanced per currency — the same ΣDEBIT=ΣCREDIT rule PostingService enforces
// later; OFF_BALANCE (nhập/xuất ngoại bảng) is N same-direction lines of one
// equal amount on nature-B accounts (balances skip the availability check).
func validateManualPostingFlow(flow string, req *financev1.PostingRequest) error {
	if flow != FlowSingleEntry && flow != FlowDoubleEntry && flow != FlowOffBalance {
		return fmt.Errorf("flow must be SINGLE_ENTRY, DOUBLE_ENTRY or OFF_BALANCE (CANCELLATION uses cancellation_request)")
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
	case FlowOffBalance:
		return validateOffBalanceShape(req.GetLines())
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

// validateOffBalanceShape: ≥1 line, every line the same direction (Nhập →
// DEBIT, Xuất → CREDIT), every amount equal, every line carries an
// account_code (already enforced for manual posting). Off-balance lines sit
// on nature-B accounts whose availability is never checked, so no balance
// rule applies here — only the same-direction memo shape.
func validateOffBalanceShape(lines []*financev1.PostingLine) error {
	if len(lines) == 0 {
		return fmt.Errorf("OFF_BALANCE requires at least 1 line")
	}
	direction := lines[0].GetDirection()
	if direction != "DEBIT" && direction != "CREDIT" {
		return fmt.Errorf("OFF_BALANCE requires DEBIT or CREDIT lines, got %q", direction)
	}
	amount := lines[0].GetAmountMinor()
	if amount <= 0 {
		return fmt.Errorf("OFF_BALANCE requires positive amounts, line 1 is %d", amount)
	}
	for i, l := range lines {
		if l.GetDirection() != direction {
			return fmt.Errorf("OFF_BALANCE requires all lines in one direction (%s), line %d is %s", direction, i+1, l.GetDirection())
		}
		if l.GetAmountMinor() != amount {
			return fmt.Errorf("OFF_BALANCE requires equal amounts on every line (line 1 is %d, line %d is %d)", amount, i+1, l.GetAmountMinor())
		}
	}
	return nil
}

// validateCancellationShape: no posting request — the flow references a
// POSTED journal entry by its human entry_no and carries a reason (the
// reversal description). AccountingDate, when sent, must be a real date.
func validateCancellationShape(in *CancellationCaseInput) error {
	if in == nil {
		return fmt.Errorf("cancellation_request is required")
	}
	if strings.TrimSpace(in.ReferenceEntryNo) == "" {
		return fmt.Errorf("cancellation_request.reference_entry_no is required")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return fmt.Errorf("cancellation_request.reason is required")
	}
	if in.AccountingDate != "" {
		if _, err := time.Parse("2006-01-02", in.AccountingDate); err != nil {
			return fmt.Errorf("cancellation_request.accounting_date must be YYYY-MM-DD")
		}
	}
	return nil
}

// CreatePostingCase validates the posting structurally, previews it through
// PostingService.ValidatePosting (COA resolution + balance rules), then
// creates and submits the maker-checker workflow case. The case idempotency
// key is generated BEFORE CreateCase so a retry after a partial failure
// replays instead of opening a second case. CANCELLATION skips the posting
// preview (there is no posting request) and instead fails fast on an
// unknown reference entry — the authoritative status check still happens in
// the workflow init/validate workers. CLOSING (iteration 11) builds its
// posting server-side from INC/EXP rows and lives in closing_case_service.go.
func (s *PostingCaseService) CreatePostingCase(ctx context.Context, tenantID, actor string, in PostingCaseInput) (*PostingCaseResult, error) {
	if in.Flow == FlowCancellation {
		return s.createCancellationCase(ctx, tenantID, actor, in)
	}
	if in.Flow == FlowClosing {
		return s.CreateClosingCase(ctx, tenantID, actor, in.Closing)
	}
	if in.Flow == FlowFund {
		return s.CreateFundCase(ctx, tenantID, actor, in.Fund)
	}
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
	// Actor metadata (the authenticated maker) — preserved when the caller
	// pinned their own; the workflow worker merges the case's trader_* keys
	// on top when deserializing.
	if in.PostingRequest.Metadata == nil {
		in.PostingRequest.Metadata = map[string]string{}
	}
	if in.PostingRequest.Metadata["actor"] == "" {
		in.PostingRequest.Metadata["actor"] = actor
	}
	caseType := CaseTypeFinSingleEntry
	title := "Bút toán lẻ — "
	switch in.Flow {
	case FlowDoubleEntry:
		caseType = CaseTypeFinDoubleEntry
		title = "Bút toán kép — "
	case FlowOffBalance:
		caseType = CaseTypeFinOffBalance
		title = "Ngoại bảng — "
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

// createCancellationCase creates the FIN_TXN_CANCEL_V2 maker-checker case
// that reverses one POSTED journal entry on approval. There is no posting
// request to preview; the reference entry is looked up here only to fail
// fast on a typo — the init/validate workers re-check status/reversal
// against the authoritative read.
func (s *PostingCaseService) createCancellationCase(ctx context.Context, tenantID, actor string, in PostingCaseInput) (*PostingCaseResult, error) {
	if err := validateCancellationShape(in.Cancellation); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	// Fail fast: the referenced entry must exist for this tenant. Status /
	// reversed_by guards run in the workflow workers (GetJournalEntry), which
	// re-check right before the reversal.
	entryNo, err := strconv.ParseInt(strings.TrimSpace(in.Cancellation.ReferenceEntryNo), 10, 64)
	if err != nil || entryNo <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput,
			fmt.Sprintf("cancellation_request.reference_entry_no %q is not a valid journal entry number", in.Cancellation.ReferenceEntryNo))
	}
	ref, err := s.posting.FindEntryRefByNo(ctx, tenantID, entryNo)
	if err != nil {
		if errors.Is(err, ErrJournalEntryNotFound) {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput,
				fmt.Sprintf("cancellation_request.reference_entry_no %d does not exist", entryNo))
		}
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	if ref.Status != "POSTED" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput,
			fmt.Sprintf("cancellation_request.reference_entry_no %d has status %s; only POSTED entries can be cancelled", entryNo, ref.Status))
	}

	// The FE may pin the idempotency key; otherwise generate one so a partial
	// create+submit failure never opens a second case for one attempt.
	idempotencyKey := in.Cancellation.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("fin-cancellation-%s", newRandomUUID())
	}
	title := "Hủy giao dịch — JE-" + in.Cancellation.ReferenceEntryNo

	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseTypeFinCancellation,
		CaseCode:          "",
		Title:             title,
		PrimaryObjectType: primaryObjectTypeFinPosting,
		PrimaryObjectID:   in.Cancellation.ReferenceEntryNo,
		DomainService:     domainServiceFinance,
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    idempotencyKey,
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}

	if _, err := s.workflow.SubmitCase(ctx, caseCreated.GetId(), actor, map[string]any{
		"flow":                  FlowCancellation,
		"postingIdempotencyKey": idempotencyKey,
		"cancellationRequest":   cancellationRequestVariables(in.Cancellation),
	}, idempotencyKey+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	return &PostingCaseResult{
		CaseID:   caseCreated.GetId(),
		CaseCode: caseCreated.GetCaseCode(),
	}, nil
}

// cancellationRequestVariables serializes the cancellation request into the
// camelCase JSON object the cancellation workers deserialize (mirror of
// postingRequestVariables).
func cancellationRequestVariables(in *CancellationCaseInput) map[string]any {
	vars := map[string]any{
		"referenceEntryNo": in.ReferenceEntryNo,
		"reason":           in.Reason,
	}
	if v := in.AccountingDate; v != "" {
		vars["accountingDate"] = v
	}
	if v := in.IdempotencyKey; v != "" {
		vars["idempotencyKey"] = v
	}
	if t := in.Trader; t != nil {
		trader := map[string]any{}
		if v := t.ObjectType; v != "" {
			trader["objectType"] = v
		}
		if v := t.ObjectCode; v != "" {
			trader["objectCode"] = v
		}
		if v := t.ObjectName; v != "" {
			trader["objectName"] = v
		}
		if v := t.IDNumber; v != "" {
			trader["idNumber"] = v
		}
		if v := t.IssueDate; v != "" {
			trader["issueDate"] = v
		}
		if v := t.IssuePlace; v != "" {
			trader["issuePlace"] = v
		}
		if v := t.Address; v != "" {
			trader["address"] = v
		}
		vars["trader"] = trader
	}
	return vars
}

// postingRequestVariables serializes the posting request into the camelCase
// JSON object the manual posting workers deserialize (map[string]any keeps
// structpb happy in SubmitCase).
func postingRequestVariables(req *financev1.PostingRequest) map[string]any {
	lines := make([]any, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		line := map[string]any{
			"lineNo":      l.GetLineNo(),
			"direction":   l.GetDirection(),
			"amountMinor": l.GetAmountMinor(),
			"accountCode": l.GetAccountCode(),
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
	if meta := req.GetMetadata(); len(meta) > 0 {
		// Actor (+ later trader) stamp rides the case variables so the
		// workers rebuild an identical PostingRequest on Reserve/Post.
		rawMeta := map[string]any{}
		for k, v := range meta {
			rawMeta[k] = v
		}
		vars["metadata"] = rawMeta
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
