package service

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func buildPostingSheet(t *testing.T, rows [][]string) *bytes.Reader {
	t.Helper()
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	for i, row := range rows {
		for j, v := range row {
			cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				t.Fatalf("set cell: %v", err)
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write buffer: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestParsePostingSheetMajorAndMinorAmounts(t *testing.T) {
	r := buildPostingSheet(t, [][]string{
		{"direction", "account_code", "amount", "description", "currency_code"},
		{"DEBIT", "1011", "1,500,000", "Thu tiền mặt", "VND"},
		{"CREDIT", "1311", "1500000", "Giảm nợ", "VND"},
		{"", "", "", "", ""},
	})
	parsed, err := parsePostingSheet(r)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(parsed.Lines))
	}
	if parsed.Lines[0].Direction != "DEBIT" || parsed.Lines[0].AccountCode != "1011" {
		t.Fatalf("line 1 = %+v", parsed.Lines[0])
	}
	if parsed.Lines[0].AmountMinor != 150_000_000 {
		t.Fatalf("amount_minor = %d, want 150000000", parsed.Lines[0].AmountMinor)
	}
	if parsed.CurrencyCode != "VND" {
		t.Fatalf("currency = %q, want VND", parsed.CurrencyCode)
	}
}

func TestParsePostingSheetMinorColumnAndDate(t *testing.T) {
	r := buildPostingSheet(t, [][]string{
		{"direction", "account_code", "amount_minor", "accounting_date"},
		{"CR", "5111", "250000", "2026-09-30"},
		{"DR", "1011", "250000", "2026-09-30"},
	})
	parsed, err := parsePostingSheet(r)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(parsed.Lines))
	}
	if parsed.Lines[1].Direction != "DEBIT" || parsed.Lines[0].Direction != "CREDIT" {
		t.Fatalf("direction normalization failed: %+v", parsed.Lines)
	}
	if parsed.AccountingDate != "2026-09-30" {
		t.Fatalf("accounting_date = %q", parsed.AccountingDate)
	}
}

func TestParsePostingSheetRejectsMissingColumns(t *testing.T) {
	r := buildPostingSheet(t, [][]string{
		{"direction", "account_code"},
		{"DEBIT", "1011"},
	})
	if _, err := parsePostingSheet(r); err == nil {
		t.Fatal("missing amount column must fail")
	}
}

func TestParsePostingSheetRejectsNonPositiveAmount(t *testing.T) {
	r := buildPostingSheet(t, [][]string{
		{"direction", "account_code", "amount"},
		{"DEBIT", "1011", "0"},
	})
	if _, err := parsePostingSheet(r); err == nil {
		t.Fatal("non-positive amount must fail")
	}
}
