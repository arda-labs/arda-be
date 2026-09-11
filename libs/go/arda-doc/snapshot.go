package ardadoc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// XLSXMime is the content type stored for filled workbook snapshots.
const XLSXMime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// PDFMime is the content type stored for rendered PDF snapshots.
const PDFMime = "application/pdf"

// SnapshotPipeline is the render → convert → persist chain domain services
// call at workflow milestones: it produces the artifact with Gotenberg,
// stores it in media-service, and attaches it to the owning entity so it is
// listed among the case's attachments permanently.
type SnapshotPipeline struct {
	gotenberg *GotenbergClient
	media     *MediaUploader
}

// NewSnapshotPipeline wires the conversion client and media uploader.
func NewSnapshotPipeline(gotenberg *GotenbergClient, media *MediaUploader) *SnapshotPipeline {
	return &SnapshotPipeline{gotenberg: gotenberg, media: media}
}

// SnapshotOptions describes the artifact and its owning entity. Filename is
// the stored name users see in the attachment list.
type SnapshotOptions struct {
	Module     string // owning module, e.g. "loan"
	EntityType string // owner entity type, e.g. "business_case"
	EntityID   string // owner entity id, e.g. the case public id
	Filename   string // stored filename, e.g. "LNM-0001-decision.pdf"
	PDFOptions *PDFOptions
}

func (o SnapshotOptions) validate() error {
	switch {
	case strings.TrimSpace(o.Module) == "":
		return fmt.Errorf("module is required")
	case strings.TrimSpace(o.EntityType) == "":
		return fmt.Errorf("entity_type is required")
	case strings.TrimSpace(o.EntityID) == "":
		return fmt.Errorf("entity_id is required")
	case strings.TrimSpace(o.Filename) == "":
		return fmt.Errorf("filename is required")
	}
	return nil
}

// SnapshotPDFFromHTML renders htmlTemplate with data, converts the result to
// PDF through Gotenberg Chromium, stores it in media-service, and attaches it
// to the owning entity. srcHeader carries the verified gateway identity
// headers of the request that triggered the snapshot.
func (p *SnapshotPipeline) SnapshotPDFFromHTML(ctx context.Context, srcHeader http.Header, htmlTemplate string, data any, opt SnapshotOptions) (string, error) {
	if err := opt.validate(); err != nil {
		return "", err
	}
	htmlBytes, err := RenderHTMLTemplate(htmlTemplate, data)
	if err != nil {
		return "", fmt.Errorf("render document template: %w", err)
	}
	pdfBytes, err := p.gotenberg.HTMLToPDF(ctx, htmlBytes, opt.PDFOptions)
	if err != nil {
		return "", fmt.Errorf("convert document to pdf: %w", err)
	}
	return p.storeAndAttach(ctx, srcHeader, pdfBytes, PDFMime, opt)
}

// SnapshotXLSX fills the xlsx template with values/tables, stores the
// workbook in media-service, and attaches it to the owning entity. Use this
// when the mẫu biểu must stay editable as a spreadsheet; convert through
// Gotenberg OfficeToPDF first if a PDF snapshot is required.
func (p *SnapshotPipeline) SnapshotXLSX(ctx context.Context, srcHeader http.Header, xlsxTemplate []byte, values map[string]any, tables map[string][][]any, opt SnapshotOptions) (string, error) {
	if err := opt.validate(); err != nil {
		return "", err
	}
	xlsxBytes, err := FillXLSXTemplate(xlsxTemplate, values, tables)
	if err != nil {
		return "", fmt.Errorf("fill document template: %w", err)
	}
	return p.storeAndAttach(ctx, srcHeader, xlsxBytes, XLSXMime, opt)
}

func (p *SnapshotPipeline) storeAndAttach(ctx context.Context, srcHeader http.Header, content []byte, contentType string, opt SnapshotOptions) (string, error) {
	publicID, err := p.media.Upload(ctx, srcHeader, opt.Filename, contentType, opt.Module, content)
	if err != nil {
		return "", err
	}
	if err := p.media.Attach(ctx, srcHeader, []string{publicID}, opt.EntityType, opt.EntityID); err != nil {
		return "", fmt.Errorf("attach snapshot %s: %w", publicID, err)
	}
	return publicID, nil
}
