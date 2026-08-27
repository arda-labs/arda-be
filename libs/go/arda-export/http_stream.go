package ardaexport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ExportFormat specifies the output format.
type ExportFormat string

const (
	FormatXLSX ExportFormat = "xlsx"
	FormatCSV  ExportFormat = "csv"
)

// NormalizeFormat parses format string with fallback to xlsx.
func NormalizeFormat(formatStr string) ExportFormat {
	f := strings.ToLower(strings.TrimSpace(formatStr))
	if f == "csv" {
		return FormatCSV
	}
	return FormatXLSX
}

// ServeStreamHTTP writes directly to http.ResponseWriter with Chunked Transfer Encoding using io.Pipe.
func ServeStreamHTTP(
	w http.ResponseWriter,
	r *http.Request,
	format ExportFormat,
	filename string,
	streamFunc func(ctx context.Context, w io.Writer) error,
) error {
	ctx := r.Context()

	var contentType string
	var ext string
	if format == FormatCSV {
		contentType = "text/csv; charset=utf-8"
		ext = ".csv"
	} else {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		ext = ".xlsx"
	}

	finalFilename := strings.TrimSpace(filename)
	if finalFilename == "" {
		finalFilename = "export"
	}
	if !strings.HasSuffix(strings.ToLower(finalFilename), ext) {
		finalFilename += ext
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, finalFilename))
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	pr, pw := io.Pipe()

	errChan := make(chan error, 1)

	go func() {
		defer pw.Close()
		err := streamFunc(ctx, pw)
		if err != nil {
			_ = pw.CloseWithError(err)
			errChan <- err
			return
		}
		errChan <- nil
	}()

	// Stream from pipe to response
	_, copyErr := io.Copy(w, pr)

	streamErr := <-errChan
	if streamErr != nil {
		return streamErr
	}
	return copyErr
}
