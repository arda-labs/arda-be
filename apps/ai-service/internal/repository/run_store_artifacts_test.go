package repository

import (
	"encoding/json"
	"testing"
)

func TestSelectArtifactsPrefersRenderChart(t *testing.T) {
	raw := []byte(`[
		{"tool_name":"execute","result":{"columns":["a"],"rows":[[1]],"chart":{"type":"bar"}}},
		{"tool_name":"renderChart","result":{"render":"report","report_name":"R","columns":["a"],"rows":[[1]],"chart":{"type":"bar"}}}
	]`)
	got := selectArtifacts(raw)
	if len(got) != 1 {
		t.Fatalf("got %d artifacts, want 1 (renderChart preferred)", len(got))
	}
	if !hasJSONKey(got[0], "render") {
		t.Fatalf("expected the renderChart payload, got %s", got[0])
	}
}

func TestSelectArtifactsFallsBackToExecuteChart(t *testing.T) {
	raw := []byte(`[
		{"tool_name":"execute","result":{"columns":["a"],"rows":[[1]]}},
		{"tool_name":"execute","result":{"columns":["a"],"rows":[[2]],"chart":{"type":"line"}}}
	]`)
	got := selectArtifacts(raw)
	if len(got) != 1 {
		t.Fatalf("got %d artifacts, want 1", len(got))
	}
	var payload map[string]any
	if err := json.Unmarshal(got[0], &payload); err != nil {
		t.Fatalf("artifact not JSON: %v", err)
	}
	if _, ok := payload["chart"]; !ok {
		t.Fatalf("expected the chart-bearing execute artifact, got %s", got[0])
	}
}

func TestSelectArtifactsCapsAndIgnoresEmpty(t *testing.T) {
	if selectArtifacts(nil) != nil {
		t.Fatal("nil should yield nil")
	}
	if selectArtifacts([]byte("not-json")) != nil {
		t.Fatal("bad JSON should yield nil")
	}
	raw := []byte(`[
		{"tool_name":"renderChart","result":{"chart":{"type":"bar"}}},
		{"tool_name":"renderChart","result":{"chart":{"type":"line"}}},
		{"tool_name":"renderChart","result":{"chart":{"type":"pie"}}}
	]`)
	if got := selectArtifacts(raw); len(got) != 2 {
		t.Fatalf("got %d artifacts, want cap of 2", len(got))
	}
}

func TestSelectArtifactsKeepsWidgetsAlongsideCharts(t *testing.T) {
	raw := []byte(`[
		{"tool_name":"execute","result":{"columns":["a"],"rows":[[1]],"chart":{"type":"bar"}}},
		{"tool_name":"renderChart","result":{"render":"report","chart":{"type":"bar"}}},
		{"tool_name":"renderWidget","result":{"widget":true,"title":"W","html":"<p>a</p>","js":"1","data":{"a":1}}}
	]`)
	got := selectArtifacts(raw)
	if len(got) != 2 {
		t.Fatalf("got %d artifacts, want 2 (chart + widget), execute dropped", len(got))
	}
	if !hasJSONKey(got[0], "render") || !hasJSONKey(got[1], "widget") {
		t.Fatalf("order must follow finished_at: chart then widget, got %s / %s", got[0], got[1])
	}
}

func TestSelectArtifactsWidgetOnlyRun(t *testing.T) {
	raw := []byte(`[
		{"tool_name":"execute","result":{"columns":["a"],"rows":[[1]]}},
		{"tool_name":"renderWidget","result":{"widget":true,"title":"W","html":"<p>a</p>","js":"1","result_id":"r1"}}
	]`)
	got := selectArtifacts(raw)
	if len(got) != 1 || !hasJSONKey(got[0], "widget") {
		t.Fatalf("expected only the widget, got %d: %v", len(got), got)
	}
}

func TestSelectArtifactsExecuteWidgetShapeNeverReplaysAsWidget(t *testing.T) {
	// execute() output is model-authored JS; a {"widget":true,...} return value
	// must not replay as a widget (it would bypass renderWidget validation and
	// the feature flag). The otherwise-renderable chart artifact still replays.
	raw := []byte(`[
		{"tool_name":"execute","result":{"widget":true,"html":"<p>x</p>","js":"1","columns":["a"],"rows":[[1]],"chart":{"type":"bar"}}}
	]`)
	got := selectArtifacts(raw)
	if len(got) != 1 {
		t.Fatalf("got %d artifacts, want the chart-bearing execute payload", len(got))
	}
	if hasJSONKey(got[0], "widget") {
		t.Fatalf("execute payload must not carry a widget key: %s", got[0])
	}
	if !hasJSONKey(got[0], "chart") || !hasJSONKey(got[0], "rows") {
		t.Fatalf("the rest of the payload must survive: %s", got[0])
	}
}

func TestSelectArtifactsLastResultFallbackStripsWidgetKey(t *testing.T) {
	raw := []byte(`[
		{"tool_name":"execute","result":{"widget":true,"html":"<p>x</p>","js":"1","columns":["a"],"rows":[[1]]}}
	]`)
	got := selectArtifacts(raw)
	if len(got) != 1 {
		t.Fatalf("got %d artifacts, want 1", len(got))
	}
	if hasJSONKey(got[0], "widget") {
		t.Fatalf("widget key must be stripped: %s", got[0])
	}
}

func TestSelectArtifactsRenderWidgetWithoutWidgetKeyIsNotChosen(t *testing.T) {
	raw := []byte(`[
		{"tool_name":"renderWidget","result":{"title":"W","columns":["a"],"rows":[[1]]}}
	]`)
	if got := selectArtifacts(raw); len(got) != 0 {
		t.Fatalf("a renderWidget result lacking widget must not replay, got %s", got)
	}
}
