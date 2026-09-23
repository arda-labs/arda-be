package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func executeChart(t *testing.T, args string) (Result, error) {
	t.Helper()
	return NewChartMetaTool().Execute(context.Background(), Context{RequestID: "req-1"}, json.RawMessage(args))
}

func TestChartMetaToolBuildsPresentationPayload(t *testing.T) {
	res, err := executeChart(t, `{
		"title": "Dư nợ theo nhóm nợ",
		"chart_type": "bar",
		"categories": ["Nhóm 1", "Nhóm 3"],
		"series": [{"name": "Dư nợ", "values": [100, 40]}],
		"value_format": "amount",
		"report_code": "LOAN_PORTFOLIO",
		"period_code": "2026-08"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload["render"] != "report" {
		t.Fatalf("render = %v, want report", payload["render"])
	}
	if payload["report_code"] != "LOAN_PORTFOLIO" || payload["period_code"] != "2026-08" {
		t.Fatalf("report metadata not echoed: %v / %v", payload["report_code"], payload["period_code"])
	}
	chart, ok := payload["chart"].(map[string]any)
	if !ok || chart["type"] != "bar" {
		t.Fatalf("chart shape wrong: %v", payload["chart"])
	}
	rows, ok := payload["rows"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("rows = %v, want 2", payload["rows"])
	}
	if res.Source != "ai-chart" {
		t.Fatalf("source = %q", res.Source)
	}
}

func TestChartMetaToolRejectsMismatchedSeries(t *testing.T) {
	_, err := executeChart(t, `{
		"title": "x", "chart_type": "line",
		"categories": ["a", "b"],
		"series": [{"name": "s", "values": [1]}]
	}`)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestChartMetaToolRejectsBadChartTypeAndUnknownFields(t *testing.T) {
	if _, err := executeChart(t, `{"title":"x","chart_type":"radar","categories":["a"],"series":[{"values":[1]}]}`); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("bad chart type err = %v", err)
	}
	if _, err := executeChart(t, `{"title":"x","chart_type":"bar","categories":["a"],"series":[{"values":[1]}],"bogus":true}`); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unknown field err = %v", err)
	}
}

func TestChartMetaToolDefinition(t *testing.T) {
	def := NewChartMetaTool().Definition()
	if def.Name != "renderChart" || def.Kind != "read" {
		t.Fatalf("definition = %+v", def)
	}
	if !strings.Contains(string(def.Parameters), "chart_type") {
		t.Fatalf("parameters missing chart_type: %s", def.Parameters)
	}
}
