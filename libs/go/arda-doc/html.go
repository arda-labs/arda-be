package ardadoc

import (
	"bytes"
	"fmt"
	"html/template"
)

// RenderHTMLTemplate parses source as a Go html/template and executes it with
// data. The template owns all markup and styling; no wrapper CSS is injected,
// so a template rendered for Gotenberg Chromium must carry its own <style> and
// print layout (page size is controlled separately through PDFOptions).
func RenderHTMLTemplate(source string, data any) ([]byte, error) {
	tmpl, err := template.New("arda-doc").Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse html template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute html template: %w", err)
	}
	return buf.Bytes(), nil
}
