package tools

import (
	"bytes"
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
	res, err := executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","result_id":"t1|u1|th-1:a1","inputs_summary":"s"}`)
	if err != nil {
		t.Fatalf("result_id only should pass: %v", err)
	}
	if !strings.Contains(string(res.Data), `"result_id":"t1|u1|th-1:a1"`) {
		t.Fatalf("result_id not echoed: %s", res.Data)
	}
}

func TestWidgetMetaToolRejectsMalformedResultID(t *testing.T) {
	// Review Focus 3: the id ends up in a FE URL, so only the sandbox result-id grammar (internal/sandbox/resultid).
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
		{"js", "new RTCPeerConnection()"},
		{"js", "new webkitRTCPeerConnection()"},
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

// A widget full of "<" is valid, but the default JSON encoder escapes each one
// to "\u003c" (6 bytes), blowing the payload up before it even reaches SSE.
func TestWidgetMetaToolDoesNotHTMLEscapePayload(t *testing.T) {
	html := "<p>" + strings.Repeat("<", 20000)
	args, err := json.Marshal(map[string]any{
		"title": "t", "html": html, "js": "1", "data": map[string]any{"a": 1}, "inputs_summary": "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := executeWidget(t, string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("Result.Data = %d bytes for a %d-byte html", len(res.Data), len(html))
	if len(res.Data) >= 25000 {
		t.Fatalf("Result.Data is %d bytes, want < 25000 (HTML escaping must be off)", len(res.Data))
	}
	if bytes.Contains(res.Data, []byte(`\u003c`)) || bytes.HasSuffix(res.Data, []byte("\n")) {
		t.Fatalf("payload must carry literal < and no trailing newline")
	}
	var payload struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload.HTML != html {
		t.Fatalf("html did not round-trip (%d vs %d bytes)", len(payload.HTML), len(html))
	}
}

func TestWidgetMetaToolBoundsTitleAndSummary(t *testing.T) {
	build := func(title, summary string) string {
		raw, _ := json.Marshal(map[string]any{
			"title": title, "html": "<p>a</p>", "js": "1", "data": map[string]any{"a": 1}, "inputs_summary": summary,
		})
		return string(raw)
	}
	vi := func(n int) string { return strings.Repeat("ệ", n) } // 3 bytes per rune

	for _, tc := range []struct {
		name, title, summary string
		wantErr              string
	}{
		{"title at limit", strings.Repeat("a", widgetMaxTitleRunes), "s", ""},
		{"title at limit multibyte", vi(widgetMaxTitleRunes), "s", ""},
		{"title one over", strings.Repeat("a", widgetMaxTitleRunes+1), "s", "title"},
		{"title one over multibyte", vi(widgetMaxTitleRunes + 1), "s", "title"},
		{"summary at limit", "t", strings.Repeat("a", widgetMaxSummaryRunes), ""},
		{"summary at limit multibyte", "t", vi(widgetMaxSummaryRunes), ""},
		{"summary one over", "t", strings.Repeat("a", widgetMaxSummaryRunes+1), "inputs_summary"},
		{"summary one over multibyte", "t", vi(widgetMaxSummaryRunes + 1), "inputs_summary"},
	} {
		_, err := executeWidget(t, build(tc.title, tc.summary))
		if tc.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s: err = %v, want ErrInvalidArgument mentioning %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestWidgetMetaToolRejectsResultIDsOutsideTheGrammar(t *testing.T) {
	for _, id := range []string{"t1|u1|th1", "t1|u1|a/b:1", "t1|u1|th1:1?x=1", "t1|u1|a b:1", "t1|u1|a%2Fb:1", `t1|u1|a\"b:1`} {
		_, err := executeWidget(t, `{"title":"t","html":"<p>a</p>","js":"1","result_id":"`+id+`","inputs_summary":"s"}`)
		if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "result_id") {
			t.Errorf("%q: err = %v", id, err)
		}
	}
}
