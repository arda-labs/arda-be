package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Size ceilings for model-generated widgets. Code is bounded so one tool call
// cannot flood the transcript; data is bounded because it is a copy of tenant
// rows that lives in the conversation history.
const (
	widgetMaxCodeBytes = 48 * 1024
	widgetMaxDataBytes = 64 * 1024
)

// widgetForbidden is an early, model-facing filter so the agent can fix its
// call in the next step. It is NOT the security boundary: the FE sandbox and
// the CSP inside the iframe are. Keep it in sync with the spec §4.3.
var widgetForbidden = []string{
	"<script src", "import(", "fetch(", "xmlhttprequest", "websocket", "sendbeacon",
	"document.cookie", "window.top", "parent.location",
	"<link", "<iframe", "<object", "<embed", "<form",
}

var widgetResultIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type widgetArguments struct {
	Title         string          `json:"title"`
	HTML          string          `json:"html"`
	CSS           string          `json:"css"`
	JS            string          `json:"js"`
	Data          json.RawMessage `json:"data"`
	ResultID      string          `json:"result_id"`
	InputsSummary string          `json:"inputs_summary"`
}

// WidgetMetaTool exposes renderWidget: the model hands over a small
// self-contained HTML/CSS/JS "what-if" tool plus the data it works on, and the
// interface runs it inside a sandboxed iframe. Display-only output (tables,
// charts, catalogued reports) must keep using renderChart / markdown.
type WidgetMetaTool struct{}

func NewWidgetMetaTool() *WidgetMetaTool { return &WidgetMetaTool{} }

func (t *WidgetMetaTool) Definition() Definition {
	return Definition{
		Name:    "renderWidget",
		Version: 1,
		Kind:    "read",
		Risk:    "low",
		Description: "Render a small interactive 'what-if' tool for the user (sliders, inputs, recomputed results) on data you already computed with execute(). " +
			"Do NOT use it to display static numbers, tables, charts or catalogued reports: use markdown or renderChart for those. " +
			"html is body-only; js runs after the 'arda:data' event and reads window.ARDA_DATA; no network, no imports, no external libraries, no cookies or storage; " +
			"style only with the provided CSS variables (--background, --foreground, --card, --border, --primary, --primary-foreground, --muted-foreground, --chart-1..10, --radius). " +
			"Pass small data inline (<= 64 KB) or a result_id from execute() for larger data.",
		Timeout: 1 * time.Second,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"title": { "type": "string", "description": "Widget title in Vietnamese." },
				"html": { "type": "string", "description": "Body-only HTML: controls on top, results below." },
				"css": { "type": "string", "description": "Optional CSS using only the provided variables." },
				"js": { "type": "string", "description": "Plain JS; runs after the 'arda:data' event; reads window.ARDA_DATA; no network or imports." },
				"data": { "type": "object", "description": "Inline data the widget computes on (<= 64 KB)." },
				"result_id": { "type": "string", "description": "resultId from execute() when the data is too large to inline." },
				"inputs_summary": { "type": "string", "description": "One sentence in Vietnamese listing the parameters the user can change." }
			},
			"required": ["title", "html", "js", "inputs_summary"]
		}`),
	}
}

func (t *WidgetMetaTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	_ = ctx

	var input widgetArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Result{}, fmt.Errorf("%w: title is required", ErrInvalidArgument)
	}
	if strings.TrimSpace(input.HTML) == "" {
		return Result{}, fmt.Errorf("%w: html is required", ErrInvalidArgument)
	}
	if strings.TrimSpace(input.JS) == "" {
		return Result{}, fmt.Errorf("%w: js is required", ErrInvalidArgument)
	}
	summary := strings.TrimSpace(input.InputsSummary)
	if summary == "" {
		return Result{}, fmt.Errorf("%w: inputs_summary is required", ErrInvalidArgument)
	}

	codeBytes := len(input.HTML) + len(input.CSS) + len(input.JS)
	if codeBytes > widgetMaxCodeBytes {
		return Result{}, fmt.Errorf("%w: html+css+js is %d bytes, the limit is 48 KB", ErrInvalidArgument, codeBytes)
	}

	hasData := widgetDataPresent(input.Data)
	resultID := strings.TrimSpace(input.ResultID)
	if !hasData && resultID == "" {
		return Result{}, fmt.Errorf("%w: data or result_id is required (the widget needs something to compute on)", ErrInvalidArgument)
	}
	if hasData && len(input.Data) > widgetMaxDataBytes {
		return Result{}, fmt.Errorf("%w: data is %d bytes, the limit is 64 KB; pass result_id instead", ErrInvalidArgument, len(input.Data))
	}
	if resultID != "" && !widgetResultIDPattern.MatchString(resultID) {
		return Result{}, fmt.Errorf("%w: result_id must match [A-Za-z0-9_-]", ErrInvalidArgument)
	}

	for _, field := range []struct{ name, value string }{{"html", input.HTML}, {"js", input.JS}} {
		lower := strings.ToLower(field.value)
		for _, pattern := range widgetForbidden {
			if strings.Contains(lower, pattern) {
				return Result{}, fmt.Errorf("%w: %s contains forbidden %q (widgets run offline inside a sandbox)", ErrInvalidArgument, field.name, pattern)
			}
		}
	}

	payload := map[string]any{
		"widget":         true,
		"title":          title,
		"html":           input.HTML,
		"css":            input.CSS,
		"js":             input.JS,
		"inputs_summary": summary,
		"result_id":      resultID,
	}
	if hasData {
		payload["data"] = input.Data
	} else {
		payload["data"] = nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("encode widget: %w", err)
	}
	return Result{
		Data:      raw,
		Summary:   fmt.Sprintf("Rendered widget %q (%d bytes of code).", title, codeBytes),
		Source:    "ai-widget",
		RequestID: scope.RequestID,
		FreshAt:   time.Now().UTC(),
	}, nil
}

// widgetDataPresent reports whether the model supplied data the widget can
// compute on. Absent, null, and empty objects (including ones with inner
// whitespace such as "{ }") carry nothing, so they do not count.
func widgetDataPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return true
	}
	switch compact.String() {
	case "null", "{}":
		return false
	}
	return true
}
