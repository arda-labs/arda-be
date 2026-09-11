package ardadoc

import "encoding/json"

// TemplateKind identifies the template source format.
type TemplateKind string

const (
	// TemplateKindHTML is an HTML template rendered with html/template and
	// converted to PDF through Gotenberg Chromium. Use for print-oriented
	// documents (vouchers, decisions, receipts).
	TemplateKindHTML TemplateKind = "html"
	// TemplateKindXLSX is an Excel workbook filled in place with excelize.
	// Use for tabular statements and export-style mẫu biểu.
	TemplateKindXLSX TemplateKind = "xlsx"
)

// OutputType identifies the artifact a document pipeline produces.
type OutputType string

const (
	OutputPDF  OutputType = "pdf"
	OutputXLSX OutputType = "xlsx"
)

// DocTemplate is the storage-agnostic contract for a document template
// registry row. Domain services persist registry rows in their own config
// tables (workflow, finance, loan...); the layout blob itself lives in
// media-service and is referenced by MediaFileID. MappingSchema carries the
// domain's declared template bindings (field → source path) so the FE can
// preview what a template fills without reading Go code.
type DocTemplate struct {
	TemplateKey   string          `json:"template_key"`
	Version       int             `json:"version"`
	Kind          TemplateKind    `json:"kind"`
	Output        OutputType      `json:"output"`
	MediaFileID   string          `json:"media_file_id"`
	MappingSchema json.RawMessage `json:"mapping_schema,omitempty"`
}
