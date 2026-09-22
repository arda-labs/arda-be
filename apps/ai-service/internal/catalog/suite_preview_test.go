package catalog

import (
	"encoding/json"
	"testing"
)

func TestPresentationPreviewKeepsChartAndBoundedRows(t *testing.T) {
	rows := make([][]any, 0, 120)
	for i := 0; i < 120; i++ {
		rows = append(rows, []any{"N", i})
	}
	payload := map[string]any{
		"render":      "report",
		"report_code": "LOAN_PORTFOLIO",
		"report_name": "Dư nợ theo nhóm nợ",
		"period_code": "2026-08",
		"columns":     []string{"group", "balance"},
		"rows":        rows,
		"row_count":   len(rows),
		"chart":       map[string]any{"type": "bar", "categories": []string{"a"}},
		"kpis":        []any{},
	}
	raw, _ := json.Marshal(payload)

	preview, ok := presentationPreview(raw)
	if !ok {
		t.Fatal("presentation payload was not recognized")
	}
	if preview["chart"] == nil {
		t.Fatal("chart dropped from preview")
	}
	if got := preview["row_count"]; got != 120 {
		t.Fatalf("row_count = %v, want 120", got)
	}
	kept, _ := preview["rows"].([]any)
	if len(kept) != previewMaxRows {
		t.Fatalf("kept %d rows, want %d", len(kept), previewMaxRows)
	}
	if preview["rows_truncated"] != true {
		t.Fatal("rows_truncated flag missing")
	}
	if preview["hint"] == nil {
		t.Fatal("readResult hint missing")
	}
}

func TestPresentationPreviewIgnoresPlainObjects(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"items": []any{1, 2, 3}, "total": 3})
	if _, ok := presentationPreview(raw); ok {
		t.Fatal("plain object should not be treated as a presentation")
	}
	raw2, _ := json.Marshal(map[string]any{"chart": map[string]any{"type": "bar"}, "rows": []any{}})
	if _, ok := presentationPreview(raw2); !ok {
		t.Fatal("a payload with a chart should be treated as a presentation")
	}
}
