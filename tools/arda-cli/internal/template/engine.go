package template

import (
	"bytes"
	"fmt"
	"text/template"
)

// Engine is a thin wrapper around text/template.
type Engine struct {
	funcs template.FuncMap
}

// New creates a new template engine with the given function map (may be nil).
func New(funcs template.FuncMap) *Engine {
	return &Engine{funcs: funcs}
}

// Render applies a template string with the given data and returns the rendered output.
func (e *Engine) Render(tpl string, data any) (string, error) {
	t := template.New("").Option("missingkey=error")
	if e.funcs != nil {
		t = t.Funcs(e.funcs)
	}

	parsed, err := t.Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := parsed.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}
