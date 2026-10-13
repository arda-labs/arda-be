package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func executeWidget(t *testing.T, args string) (Result, error) {
	t.Helper()
	return NewWidgetMetaTool().Execute(context.Background(), Context{RequestID: "req-1"}, json.RawMessage(args))
}

const validWidget = `{
	"title": "Lịch trả nợ",
	"html": "<label>Lãi suất <input id=\"rate\" type=\"range\" min=\"1\" max=\"20\"></label><div id=\"out\"></div>",
	"css": "#out { color: var(--foreground); }",
	"js": "const out = document.getElementById('out'); out.textContent = String(window.ARDA_DATA.principal);",
	"data": { "principal": 1200000000 },
	"inputs_summary": "Lãi suất 1-20%"
}`

func TestWidgetMetaToolReturnsNormalizedPayload(t *testing.T) {
	res, err := executeWidget(t, validWidget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload["widget"] != true {
		t.Fatalf("widget marker missing: %v", payload)
	}
	if payload["title"] != "Lịch trả nợ" || payload["inputs_summary"] != "Lãi suất 1-20%" {
		t.Fatalf("title/summary not echoed: %v", payload)
	}
	if _, ok := payload["data"].(map[string]any); !ok {
		t.Fatalf("data not echoed: %v", payload["data"])
	}
	if res.Source != "ai-widget" || res.RequestID != "req-1" {
		t.Fatalf("result metadata wrong: %+v", res)
	}
	def := NewWidgetMetaTool().Definition()
	if def.Name != "renderWidget" || def.Kind != "read" || def.Risk != "low" {
		t.Fatalf("definition wrong: %+v", def)
	}
}

func TestWidgetMetaToolRejectsMissingRequiredFields(t *testing.T) {
	for _, tc := range []struct{ name, args, want string }{
		{"title", `{"html":"<p>a</p>","js":"1","data":{"a":1},"inputs_summary":"s"}`, "title"},
		{"html", `{"title":"t","js":"1","data":{"a":1},"inputs_summary":"s"}`, "html"},
		{"js", `{"title":"t","html":"<p>a</p>","data":{"a":1},"inputs_summary":"s"}`, "js"},
		{"summary", `{"title":"t","html":"<p>a</p>","js":"1","data":{"a":1}}`, "inputs_summary"},
		{"unknown field", `{"title":"t","html":"<p>a</p>","js":"1","data":{"a":1},"inputs_summary":"s","extra":1}`, "unknown field"},
	} {
		_, err := executeWidget(t, tc.args)
		if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want ErrInvalidArgument mentioning %q", tc.name, err, tc.want)
		}
	}
}

func TestWidgetMetaToolRequiresDataOrResultID(t *testing.T) {
	_, err := executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","inputs_summary":"s"}`)
	if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "data or result_id") {
		t.Fatalf("missing both: err = %v", err)
	}
	// Review Focus 1: an empty object carries nothing to compute on.
	_, err = executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","data":{},"inputs_summary":"s"}`)
	if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "data or result_id") {
		t.Fatalf("empty data: err = %v", err)
	}
	// Whitespace-only empty object is equally empty.
	_, err = executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","data": { },"inputs_summary":"s"}`)
	if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "data or result_id") {
		t.Fatalf("whitespace empty data: err = %v", err)
	}
	res, err := executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","result_id":"res_abc-123","inputs_summary":"s"}`)
	if err != nil {
		t.Fatalf("result_id only should pass: %v", err)
	}
	if !strings.Contains(string(res.Data), `"result_id":"res_abc-123"`) {
		t.Fatalf("result_id not echoed: %s", res.Data)
	}
}

func TestWidgetMetaToolRejectsMalformedResultID(t *testing.T) {
	// Review Focus 3: the id ends up in a FE URL, so only [A-Za-z0-9_-].
	_, err := executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","result_id":"../x?y","inputs_summary":"s"}`)
	if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "result_id") {
		t.Fatalf("err = %v", err)
	}
}

func TestWidgetMetaToolRejectsForbiddenPatterns(t *testing.T) {
	for _, tc := range []struct{ field, snippet string }{
		{"js", "fetch('/api')"},
		{"js", "FETCH('/api')"}, // Review Focus 2: case-insensitive
		{"js", "import('x')"},
		{"js", "new XMLHttpRequest()"},
		{"js", "new WebSocket('ws://x')"},
		{"js", "navigator.sendBeacon('/x')"},
		{"js", "document.cookie"},
		{"js", "window.top.location"},
		{"js", "parent.location.href"},
		{"html", "<script src=\"x.js\"></script>"},
		{"html", "<link rel=stylesheet>"},
		{"html", "<iframe src=x>"},
		{"html", "<object data=x>"},
		{"html", "<embed src=x>"},
		{"html", "<form action=x>"},
	} {
		args := map[string]any{"title": "t", "html": "<p>a</p>", "js": "1", "data": map[string]any{"a": 1}, "inputs_summary": "s"}
		args[tc.field] = tc.snippet
		raw, _ := json.Marshal(args)
		_, err := executeWidget(t, string(raw))
		if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "forbidden") {
			t.Fatalf("%s=%q: err = %v, want forbidden", tc.field, tc.snippet, err)
		}
	}
}

func TestWidgetMetaToolEnforcesSizeLimits(t *testing.T) {
	big := strings.Repeat("x", widgetMaxCodeBytes+1)
	_, err := executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"`+big+`","data":{"a":1},"inputs_summary":"s"}`)
	if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "48 KB") {
		t.Fatalf("code size: err = %v", err)
	}
	bigData := strings.Repeat("y", widgetMaxDataBytes)
	_, err = executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","data":{"v":"`+bigData+`"},"inputs_summary":"s"}`)
	if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "64 KB") {
		t.Fatalf("data size: err = %v", err)
	}
}
