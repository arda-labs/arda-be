package presentation

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestChartFromReportPicksShapes locks the deterministic chart choice:
// temporal label ⇒ line, few categories ⇒ pie, many ⇒ bar.
func TestChartFromReportPicksShapes(t *testing.T) {
	line := ChartFromReport("Diễn biến", []string{"period_code", "principal_minor"},
		[][]any{{"2026-07", int64(10)}, {"2026-08", int64(20)}, {"2026-09", int64(30)}})
	if line.Type != ChartLine || len(line.Series) != 1 || len(line.Categories) != 3 {
		t.Fatalf("temporal shape wrong: %+v", line)
	}

	pie := ChartFromReport("Cơ cấu", []string{"segment", "customer_count"},
		[][]any{{"RETAIL", int64(3)}, {"CORP", int64(1)}})
	if pie.Type != ChartPie {
		t.Fatalf("few categories should be pie, got %s", pie.Type)
	}

	bar := ChartFromReport("Xếp hạng", []string{"contract_code", "outstanding_amt_minor"},
		[][]any{{"A", 1}, {"B", 2}, {"C", 3}, {"D", 4}, {"E", 5}, {"F", 6}, {"G", 7}})
	if bar.Type != ChartBar {
		t.Fatalf("many categories should be bar, got %s", bar.Type)
	}
}

// TestChartFromReportDegradesWithReason: an unchartable shape must explain
// itself instead of emitting an empty chart.
func TestChartFromReportDegradesWithReason(t *testing.T) {
	c := ChartFromReport("X", []string{"a"}, [][]any{{1}})
	if c.Type != ChartNone || c.Reason == "" {
		t.Fatalf("expected none + reason, got %+v", c)
	}
	c = ChartFromReport("X", []string{"alpha", "beta"}, [][]any{{"a", "b"}})
	if c.Type != ChartNone || c.Reason == "" {
		t.Fatalf("no numeric column should fail with a reason, got %+v", c)
	}
}

// TestSortSeriesDesc keeps label/value pairs aligned while ranking.
func TestSortSeriesDesc(t *testing.T) {
	c := Chart{Categories: []string{"A", "B", "C"}, Series: []Series{{Name: "x", Values: []float64{1, 9, 5}}}}
	c = SortSeriesDesc(c)
	if c.Categories[0] != "B" || c.Series[0].Values[0] != 9 {
		t.Fatalf("ranking wrong: %+v", c)
	}
	if c.Categories[2] != "A" || c.Series[0].Values[2] != 1 {
		t.Fatalf("ranking tail wrong: %+v", c)
	}
}

// TestRenderHTMLEscapesAndFormats proves the print template is real HTML with
// escaped data and a numeric cell class.
func TestRenderHTMLEscapesAndFormats(t *testing.T) {
	doc := &ReportDocument{
		Title:   "Báo cáo <test>",
		Period:  "2026-09",
		Columns: []string{"segment", "customer_count"},
		Rows:    [][]any{{"RETAIL", int64(3)}},
		KPI:     []KPI{{Code: "60000.01", Label: "Số lượng khách hàng", Value: "1"}},
	}
	out, err := RenderHTML(doc)
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	html := string(out)
	if strings.Contains(html, "<test>") {
		t.Fatalf("title must be escaped: %s", html)
	}
	if !strings.Contains(html, "&lt;test&gt;") {
		t.Fatalf("escaped title missing: %s", html)
	}
	if !strings.Contains(html, `class="num"`) {
		t.Fatalf("numeric column should get the num class: %s", html)
	}
	if !strings.Contains(html, "Số lượng khách hàng") {
		t.Fatalf("KPI label missing: %s", html)
	}
}

// TestRenderXLSXProducesReadableWorkbook parses the output back so a corrupt
// workbook fails the test rather than reaching a user.
func TestRenderXLSXProducesReadableWorkbook(t *testing.T) {
	doc := &ReportDocument{
		Title:   "Huy động theo sản phẩm",
		Period:  "2026-09",
		Columns: []string{"product_code", "principal_minor"},
		Rows:    [][]any{{"DPM12M", int64(100000000)}},
		KPI:     []KPI{{Label: "Tổng tiền gửi", Value: "100000000", Unit: "VND"}},
	}
	out, _, err := RenderReport(doc, FormatXLSX)
	if err != nil {
		t.Fatalf("render xlsx: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("open rendered workbook: %v", err)
	}
	defer f.Close()
	if got, _ := f.GetCellValue("BaoCao", "A1"); got != "Huy động theo sản phẩm" {
		t.Fatalf("title cell = %q", got)
	}
}

// TestTableWritesHeaderRow keeps the document/excel table contract.
func TestTableWritesHeaderRow(t *testing.T) {
	rows := Table([]string{"a", "b"}, [][]any{{1, 2}, {3, 4}, {5}})
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want header + 3", len(rows))
	}
	if rows[0][0] != "a" || rows[0][1] != "b" {
		t.Fatalf("header wrong: %+v", rows[0])
	}
	// Short data rows are padded, never panicking.
	if rows[3][1] != nil {
		t.Fatalf("short row should pad with nil: %+v", rows[3])
	}
}
