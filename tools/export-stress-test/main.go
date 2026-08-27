package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	ardaexport "github.com/arda-labs/arda/libs/go/arda-export"
)

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func printMemStats(label string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("   ├─ [%s] HeapAlloc: %s | HeapSys: %s | NumGC: %d\n",
		label, formatBytes(m.HeapAlloc), formatBytes(m.HeapSys), m.NumGC)
}

func main() {
	rowsFlag := flag.Int("rows", 1000000, "Number of rows to generate and export (default: 1,000,000)")
	formatFlag := flag.String("format", "csv", "Export format: csv, xlsx, or both (default: csv)")
	outDirFlag := flag.String("out", "./stress_output", "Directory to save generated files")
	flag.Parse()

	totalRows := *rowsFlag
	outDir := *outDirFlag
	_ = os.MkdirAll(outDir, 0755)

	fmt.Println("================================================================================")
	fmt.Println("       ARDA ENTERPRISE: LARGE-SCALE DATA EXPORT STRESS TEST (1M ROWS)          ")
	fmt.Println("================================================================================")
	fmt.Printf(" Target Rows  : %s (%d records)\n", formatNumber(totalRows), totalRows)
	fmt.Printf(" Formats      : %s\n", *formatFlag)
	fmt.Printf(" Output Dir   : %s\n", outDir)
	fmt.Printf(" CPU Cores    : %d | Go: %s | OS/Arch: %s/%s\n", runtime.NumCPU(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Println("--------------------------------------------------------------------------------")

	printMemStats("Initial State (Baseline)")
	fmt.Println("--------------------------------------------------------------------------------")

	cols := []ardaexport.Column{
		{Header: "STT", Key: "stt", Type: ardaexport.CellTypeNumber, Width: 10},
		{Header: "Mã giao dịch (TxID)", Key: "tx_id", Type: ardaexport.CellTypeCode, Width: 24},
		{Header: "Số tài khoản (Account No)", Key: "account_no", Type: ardaexport.CellTypeCode, Width: 20},
		{Header: "Số CCCD/CMND", Key: "citizen_id", Type: ardaexport.CellTypeCode, Width: 20},
		{Header: "Họ và tên khách hàng", Key: "customer_name", Type: ardaexport.CellTypeString, Width: 28},
		{Header: "Số tiền giao dịch (VND)", Key: "amount", Type: ardaexport.CellTypeCurrency, Width: 22},
		{Header: "Trạng thái", Key: "status", Type: ardaexport.CellTypeString, Width: 18},
		{Header: "Thời gian tạo", Key: "created_at", Type: ardaexport.CellTypeDate, Width: 22},
	}

	opts := ardaexport.StreamOptions{
		Title:      "BÁO CÁO SAO KÊ GIAO DỊCH TÀI CHÍNH TOÀN HỆ THỐNG",
		SheetName:  "Transactions",
		Columns:    cols,
		Locale:     "vi-VN",
		TotalCount: totalRows,
		Metadata: map[string]string{
			"Đơn vị phát hành": "ARDA BANKING & FINANCIAL SERVICES",
			"Môi trường":      "PRODUCTION STRESS TEST",
		},
	}

	formats := []string{}
	if *formatFlag == "both" {
		formats = []string{"csv", "xlsx"}
	} else {
		formats = []string{*formatFlag}
	}

	for _, f := range formats {
		runTest(f, totalRows, outDir, opts)
	}

	fmt.Println("================================================================================")
	fmt.Println("               ALL STRESS TESTS COMPLETED SUCCESSFULLY!                         ")
	fmt.Println("================================================================================")
}

func runTest(format string, totalRows int, outDir string, opts ardaexport.StreamOptions) {
	ext := ".csv"
	if format == "xlsx" {
		ext = ".xlsx"
	}
	filePath := filepath.Join(outDir, fmt.Sprintf("stress_test_%d_rows%s", totalRows, ext))

	fmt.Printf("\n▶ BẮT ĐẦU XUẤT ĐỊNH DẠNG: [%s] (%s dòng)\n", strings.ToUpper(format), formatNumber(totalRows))
	outFile, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("❌ Không thể tạo file: %v\n", err)
		return
	}
	defer outFile.Close()

	ctx := context.Background()
	startTime := time.Now()
	lastReportTime := startTime
	lastRows := 0

	currRow := 0
	supplier := func() ([]any, error) {
		if currRow >= totalRows {
			return nil, io.EOF
		}
		currRow++

		// Telemetry reporter every 100,000 rows
		if currRow%100000 == 0 || currRow == totalRows {
			now := time.Now()
			batchElapsed := now.Sub(lastReportTime).Seconds()
			if batchElapsed <= 0 {
				batchElapsed = 0.001
			}
			batchSpeed := float64(currRow-lastRows) / batchElapsed
			percent := float64(currRow) / float64(totalRows) * 100

			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			fmt.Printf("   [%5.1f%%] %s / %s dòng  |  Tốc độ: %s dòng/giây  |  RAM: %s\n",
				percent,
				formatNumber(currRow),
				formatNumber(totalRows),
				formatNumber(int(batchSpeed)),
				formatBytes(m.HeapAlloc),
			)

			lastReportTime = now
			lastRows = currRow
		}

		// Banking test data with leading zeros
		accountNo := fmt.Sprintf("00%08d", (currRow*7)%99999999)
		citizenID := fmt.Sprintf("079201%06d", (currRow*13)%999999)
		txID := fmt.Sprintf("TXN-2026-%08d", currRow)
		customerName := fmt.Sprintf("Khách hàng số %d", currRow)
		amount := (currRow * 150000) % 5000000000

		return []any{
			currRow,
			txID,
			accountNo,
			citizenID,
			customerName,
			amount,
			"THÀNH CÔNG",
			time.Date(2026, 8, 27, 21, 45, 0, 0, time.UTC),
		}, nil
	}

	var exportErr error
	if format == "xlsx" {
		exportErr = ardaexport.StreamXLSX(ctx, outFile, opts, supplier)
	} else {
		exportErr = ardaexport.StreamCSV(ctx, outFile, opts, supplier)
	}

	if exportErr != nil {
		fmt.Printf("❌ Lỗi trong quá trình xuất: %v\n", exportErr)
		return
	}

	elapsed := time.Since(startTime)
	fi, _ := outFile.Stat()
	fileSize := uint64(fi.Size())
	avgSpeed := float64(totalRows) / elapsed.Seconds()

	fmt.Println("   ────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("   ✔ HOÀN THÀNH [%s]:\n", strings.ToUpper(format))
	fmt.Printf("     • Tổng thời gian    : %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("     • Tốc độ trung bình : %s dòng / giây\n", formatNumber(int(avgSpeed)))
	fmt.Printf("     • Kích thước tệp    : %s\n", formatBytes(fileSize))
	fmt.Printf("     • Đường dẫn tệp     : %s\n", filePath)
	printMemStats("Final State")
	fmt.Println("   ────────────────────────────────────────────────────────────────────────────")
}

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	var res []string
	for len(s) > 3 {
		res = append([]string{s[len(s)-3:]}, res...)
		s = s[:len(s)-3]
	}
	if len(s) > 0 {
		res = append([]string{s}, res...)
	}
	return strings.Join(res, ",")
}
