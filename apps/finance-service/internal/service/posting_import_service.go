package service

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/xuri/excelize/v2"
)

// PostingImportResult is the outcome of a sheet import: the created manual
// posting case plus how many lines were parsed.
type PostingImportResult struct {
	CaseID         string
	CaseCode       string
	LineCount      int
	AccountingDate string
}

// parsedSheet is the pure parser output (unit-testable without a database).
type parsedSheet struct {
	Lines          []*financev1.PostingLine
	AccountingDate string
	CurrencyCode   string
}

// ImportPostingSheet parses an XLSX posting sheet and creates a manual posting
// case (SINGLE_ENTRY by default). Columns are matched by header name in any
// order (case-insensitive): direction (DEBIT/CREDIT), account_code,
// amount (major units) or amount_minor (minor units), currency_code,
// description, counterparty_code, accounting_date.
func (s *PostingCaseService) ImportPostingSheet(ctx context.Context, tenantID, actor, flow, defaultAccountingDate string, r io.Reader) (*PostingImportResult, error) {
	parsed, err := parsePostingSheet(r)
	if err != nil {
		return nil, err
	}
	if len(parsed.Lines) == 0 {
		return nil, fmt.Errorf("sheet has no posting lines")
	}
	accountingDate := strings.TrimSpace(defaultAccountingDate)
	if parsed.AccountingDate != "" {
		accountingDate = parsed.AccountingDate
	}
	if accountingDate == "" {
		return nil, fmt.Errorf("accounting_date is required (request or accounting_date column)")
	}
	if strings.TrimSpace(flow) == "" {
		flow = FlowSingleEntry
	}

	currency := parsed.CurrencyCode
	if currency == "" {
		currency = "VND"
	}
	posting := &financev1.PostingRequest{
		AccountingDate: accountingDate,
		CurrencyCode:   currency,
		Description:    "Sheet import",
		Lines:          parsed.Lines,
	}
	result, err := s.CreatePostingCase(ctx, tenantID, actor, PostingCaseInput{
		Flow:           flow,
		PostingRequest: posting,
	})
	if err != nil {
		return nil, err
	}
	return &PostingImportResult{
		CaseID:         result.CaseID,
		CaseCode:       result.CaseCode,
		LineCount:      len(parsed.Lines),
		AccountingDate: accountingDate,
	}, nil
}

// parsePostingSheet reads the first worksheet into posting lines. It is
// deliberately DB-free so the column mapping is unit-testable.
func parsePostingSheet(r io.Reader) (*parsedSheet, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("workbook has no sheet")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read sheet: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("sheet needs a header row and at least one data row")
	}

	cols := map[string]int{}
	for i, h := range rows[0] {
		cols[normalizeHeader(h)] = i
	}
	directionIdx, ok := cols["direction"]
	if !ok {
		directionIdx, ok = cols["debit_credit"]
	}
	accountIdx, okAccount := cols["account_code"]
	if !okAccount {
		accountIdx, okAccount = cols["account"]
	}
	_, hasAmountMinor := cols["amount_minor"]
	amountIdx, hasAmount := cols["amount"]
	if !ok || !okAccount || (!hasAmount && !hasAmountMinor) {
		return nil, fmt.Errorf("missing required columns: direction, account_code, amount/amount_minor")
	}

	cell := func(row []string, idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	out := &parsedSheet{Lines: []*financev1.PostingLine{}}
	lineNo := int32(1)
	for _, row := range rows[1:] {
		account := cell(row, accountIdx)
		direction := normalizeDirection(cell(row, directionIdx))
		if account == "" && direction == "" {
			continue // blank separator row
		}
		if account == "" || direction == "" {
			return nil, fmt.Errorf("row %d: account_code and direction are required", lineNo+1)
		}
		amountMinor, err := parseAmount(row, cols, hasAmountMinor, amountIdx)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", lineNo+1, err)
		}
		if amountMinor <= 0 {
			return nil, fmt.Errorf("row %d: amount must be > 0", lineNo+1)
		}

		line := &financev1.PostingLine{
			LineNo:      lineNo,
			Direction:   direction,
			AmountMinor: amountMinor,
			AccountCode: account,
		}
		if idx, ok := cols["currency_code"]; ok {
			line.CurrencyCode = cell(row, idx)
		}
		if idx, ok := cols["description"]; ok {
			line.Description = cell(row, idx)
		}
		if idx, ok := cols["counterparty_code"]; ok {
			line.CounterpartyCode = cell(row, idx)
		}
		if idx, ok := cols["counterparty_name"]; ok {
			line.CounterpartyName = cell(row, idx)
		}
		if idx, ok := cols["accounting_date"]; ok {
			if d := cell(row, idx); d != "" {
				out.AccountingDate = d
			}
		}
		if line.CurrencyCode != "" && out.CurrencyCode == "" {
			out.CurrencyCode = line.CurrencyCode
		}
		out.Lines = append(out.Lines, line)
		lineNo++
	}
	return out, nil
}

func parseAmount(row []string, cols map[string]int, hasAmountMinor bool, amountIdx int) (int64, error) {
	cell := func(idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}
	if hasAmountMinor {
		raw := stripNumber(cell(cols["amount_minor"]))
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("amount_minor %q is not an integer", cell(cols["amount_minor"]))
		}
		return v, nil
	}
	raw := stripNumber(cell(amountIdx))
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not a number", cell(amountIdx))
	}
	return int64(math.Round(v * 100)), nil
}

// stripNumber removes thousands separators accepted in hand-authored sheets.
func stripNumber(v string) string {
	v = strings.ReplaceAll(v, ",", "")
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "\u00a0", "")
	return v
}

func normalizeHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.ReplaceAll(h, " ", "_")
	h = strings.ReplaceAll(h, "-", "_")
	return h
}

func normalizeDirection(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "DEBIT", "DR", "NỢ", "NO", "N":
		return "DEBIT"
	case "CREDIT", "CR", "CÓ", "CO", "C":
		return "CREDIT"
	default:
		return ""
	}
}
