package ardadoc

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderHTMLTemplate(t *testing.T) {
	out, err := RenderHTMLTemplate("<p>Xin chào {{.Name}}</p>", map[string]string{"Name": "Arda"})
	if err != nil {
		t.Fatalf("RenderHTMLTemplate: %v", err)
	}
	if want := "<p>Xin chào Arda</p>"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestRenderHTMLTemplateEscapes(t *testing.T) {
	out, err := RenderHTMLTemplate("<p>{{.}}</p>", "<b>&raw</b>")
	if err != nil {
		t.Fatalf("RenderHTMLTemplate: %v", err)
	}
	if strings.Contains(string(out), "<b>") {
		t.Errorf("html template must escape data, got %q", out)
	}
}

func newFakeGotenberg(t *testing.T, status int, respond []byte, captured *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			(*captured)["path"] = r.URL.Path
			(*captured)["content_type"] = r.Header.Get("Content-Type")
			body, _ := io.ReadAll(r.Body)
			(*captured)["body"] = string(body)
		}
		w.WriteHeader(status)
		_, _ = w.Write(respond)
	}))
}

func parseMultipartField(t *testing.T, contentType, body string, field string) (filename string, value string) {
	t.Helper()
	mr := multipart.NewReader(strings.NewReader(body), parseBoundary(t, contentType))
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return "", ""
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		if part.FormName() == field {
			data, _ := io.ReadAll(part)
			return part.FileName(), string(data)
		}
	}
}

func parseBoundary(t *testing.T, contentType string) string {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("no boundary in content type")
	}
	return boundary
}

func TestGotenbergHTMLToPDF(t *testing.T) {
	captured := map[string]string{}
	server := newFakeGotenberg(t, http.StatusOK, []byte("%PDF-fake"), &captured)
	defer server.Close()

	client, err := NewGotenbergClient(server.URL)
	if err != nil {
		t.Fatalf("NewGotenbergClient: %v", err)
	}
	pdf, err := client.HTMLToPDF(context.Background(), []byte("<html>hoá đơn</html>"), &PDFOptions{
		PaperWidth: "8.27in", PaperHeight: "11.69in", Landscape: true, Scale: 1.5,
	})
	if err != nil {
		t.Fatalf("HTMLToPDF: %v", err)
	}
	if string(pdf) != "%PDF-fake" {
		t.Errorf("pdf = %q", pdf)
	}
	if captured["path"] != "/forms/chromium/convert/html" {
		t.Errorf("path = %q", captured["path"])
	}
	filename, value := parseMultipartField(t, captured["content_type"], captured["body"], "files")
	if filename != "index.html" || value != "<html>hoá đơn</html>" {
		t.Errorf("file part = %q,%q", filename, value)
	}
	if _, v := parseMultipartField(t, captured["content_type"], captured["body"], "landscape"); v != "true" {
		t.Errorf("landscape field = %q", v)
	}
	if _, v := parseMultipartField(t, captured["content_type"], captured["body"], "scale"); v != "1.5" {
		t.Errorf("scale field = %q", v)
	}
}

func TestGotenbergOfficeToPDF(t *testing.T) {
	captured := map[string]string{}
	server := newFakeGotenberg(t, http.StatusOK, []byte("%PDF-fake"), &captured)
	defer server.Close()

	client, _ := NewGotenbergClient(server.URL)
	pdf, err := client.OfficeToPDF(context.Background(), "template.docx", []byte("OOXML"))
	if err != nil {
		t.Fatalf("OfficeToPDF: %v", err)
	}
	if string(pdf) != "%PDF-fake" {
		t.Errorf("pdf = %q", pdf)
	}
	if captured["path"] != "/forms/libreoffice/convert" {
		t.Errorf("path = %q", captured["path"])
	}
	filename, _ := parseMultipartField(t, captured["content_type"], captured["body"], "files")
	if filename != "template.docx" {
		t.Errorf("filename = %q", filename)
	}
}

func TestGotenbergErrorStatus(t *testing.T) {
	server := newFakeGotenberg(t, http.StatusInternalServerError, []byte("boom"), nil)
	defer server.Close()

	client, _ := NewGotenbergClient(server.URL)
	if _, err := client.HTMLToPDF(context.Background(), []byte("<html/>"), nil); err == nil {
		t.Fatal("expected error for status 500")
	}
}

func TestNewGotenbergClientRejectsBadURL(t *testing.T) {
	if _, err := NewGotenbergClient("not-a-url"); err == nil {
		t.Fatal("expected error for invalid base url")
	}
}
