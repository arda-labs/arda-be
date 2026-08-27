package ardaexport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestStreamXLSX_BankingIntegrity(t *testing.T) {
	ctx := context.Background()

	cols := []Column{
		{Header: "Mã định danh", Key: "code", Type: CellTypeCode},
		{Header: "Họ và tên", Key: "name", Type: CellTypeString},
		{Header: "Số dư khả dụng", Key: "balance", Type: CellTypeCurrency},
		{Header: "Trạng thái", Key: "status", Type: CellTypeString},
		{Header: "Đã xác thực", Key: "verified", Type: CellTypeBoolean},
		{Header: "Ngày mở tài khoản", Key: "created_at", Type: CellTypeDate},
	}

	opts := StreamOptions{
		Title:      "BÁO CÁO TÀI KHOẢN KHÁCH HÀNG",
		SheetName:  "Accounts",
		Columns:    cols,
		Locale:     "vi-VN",
		TotalCount: 5,
	}

	mockRows := [][]any{
		{"0012345678", "Nguyễn Văn A", 150000000, "Đang hoạt động", true, time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)},
		{"0009876543", "Trần Thị B", 8500000, "Đang hoạt động", false, time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)},
		{"0000012345", "Lê Văn C", 0, "Đã vô hiệu", true, time.Date(2026, 3, 5, 9, 15, 0, 0, time.UTC)},
	}

	rowIdx := 0
	supplier := func() ([]any, error) {
		if rowIdx >= len(mockRows) {
			return nil, io.EOF
		}
		r := mockRows[rowIdx]
		rowIdx++
		return r, nil
	}

	var buf bytes.Buffer
	err := StreamXLSX(ctx, &buf, opts, supplier)
	if err != nil {
		t.Fatalf("StreamXLSX returned error: %v", err)
	}

	// Verify XLSX validity by opening it
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("Failed to parse generated XLSX: %v", err)
	}
	defer f.Close()

	// 1. Verify Title Banner (A1)
	titleVal, err := f.GetCellValue("Accounts", "A1")
	if err != nil || !strings.Contains(titleVal, "BÁO CÁO TÀI KHOẢN KHÁCH HÀNG") {
		t.Errorf("Expected title in A1, got %q (err: %v)", titleVal, err)
	}

	// 2. Verify Headers (Row 4)
	header1, _ := f.GetCellValue("Accounts", "A4")
	if header1 != "Mã định danh" {
		t.Errorf("Expected A4 to be 'Mã định danh', got %q", header1)
	}

	// 3. Verify Code Leading Zeroes Preservation (Row 5 -> A5)
	codeVal, _ := f.GetCellValue("Accounts", "A5")
	if codeVal != "0012345678" {
		t.Errorf("Leading zeros truncated in A5! Expected '0012345678', got %q", codeVal)
	}

	codeVal2, _ := f.GetCellValue("Accounts", "A7")
	if codeVal2 != "0000012345" {
		t.Errorf("Leading zeros truncated in A7! Expected '0000012345', got %q", codeVal2)
	}

	// 4. Verify Boolean formatting in Vietnamese
	boolVal1, _ := f.GetCellValue("Accounts", "E5")
	if boolVal1 != "Có" {
		t.Errorf("Expected boolean E5 to be 'Có', got %q", boolVal1)
	}
	boolVal2, _ := f.GetCellValue("Accounts", "E6")
	if boolVal2 != "Không" {
		t.Errorf("Expected boolean E6 to be 'Không', got %q", boolVal2)
	}
}

func TestStreamCSV_UTF8BOM(t *testing.T) {
	ctx := context.Background()

	cols := []Column{
		{Header: "Mã", Key: "code", Type: CellTypeCode},
		{Header: "Họ và tên", Key: "name", Type: CellTypeString},
	}

	opts := StreamOptions{
		Columns: cols,
		Locale:  "vi-VN",
	}

	called := false
	supplier := func() ([]any, error) {
		if called {
			return nil, io.EOF
		}
		called = true
		return []any{"00123", "Nguyễn Văn Đạt"}, nil
	}

	var buf bytes.Buffer
	err := StreamCSV(ctx, &buf, opts, supplier)
	if err != nil {
		t.Fatalf("StreamCSV returned error: %v", err)
	}

	content := buf.String()
	// Must start with UTF-8 BOM
	if !strings.HasPrefix(content, "\uFEFF") {
		t.Errorf("CSV missing UTF-8 BOM prefix")
	}

	if !strings.Contains(content, "Nguyễn Văn Đạt") {
		t.Errorf("CSV missing Vietnamese text")
	}
}

func BenchmarkStreamXLSX_10k_Rows(b *testing.B) {
	ctx := context.Background()

	cols := []Column{
		{Header: "Mã KH", Key: "code", Type: CellTypeCode},
		{Header: "Họ tên", Key: "name", Type: CellTypeString},
		{Header: "Số dư", Key: "balance", Type: CellTypeCurrency},
		{Header: "Trạng thái", Key: "status", Type: CellTypeString},
		{Header: "Ngày tạo", Key: "created_at", Type: CellTypeDate},
	}

	opts := StreamOptions{
		Title:     "BENCHMARK 10K ROWS",
		SheetName: "Data",
		Columns:   cols,
		Locale:    "vi-VN",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		curr := 0
		supplier := func() ([]any, error) {
			if curr >= 10000 {
				return nil, io.EOF
			}
			curr++
			return []any{
				fmt.Sprintf("00%08d", curr),
				fmt.Sprintf("Khách hàng số %d", curr),
				curr * 1000,
				"ACTIVE",
				time.Now(),
			}, nil
		}

		err := StreamXLSX(ctx, io.Discard, opts, supplier)
		if err != nil {
			b.Fatalf("Benchmark error: %v", err)
		}
	}
}
