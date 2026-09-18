package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/arda-labs/arda/apps/media-service/internal/config"
	"github.com/arda-labs/arda/apps/media-service/internal/domain"
	"github.com/arda-labs/arda/apps/media-service/internal/storage"
)

type fakeStorage struct {
	objects  map[string][]byte
	putCount int
}

func newFakeStorage(objects map[string][]byte) *fakeStorage {
	return &fakeStorage{objects: objects}
}

func (f *fakeStorage) PresignPutObject(context.Context, storage.PresignPutInput) (storage.PresignedURL, error) {
	return storage.PresignedURL{}, errors.New("not implemented")
}

func (f *fakeStorage) PresignGetObject(context.Context, storage.PresignGetInput) (storage.PresignedURL, error) {
	return storage.PresignedURL{}, errors.New("not implemented")
}

func (f *fakeStorage) HeadObject(_ context.Context, bucket, key string) (storage.ObjectInfo, error) {
	data, ok := f.objects[bucket+"/"+key]
	if !ok {
		return storage.ObjectInfo{}, errors.New("not found")
	}
	return storage.ObjectInfo{SizeBytes: int64(len(data))}, nil
}

func (f *fakeStorage) PutObject(_ context.Context, bucket, key string, body io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.objects[bucket+"/"+key] = data
	f.putCount++
	return nil
}

func (f *fakeStorage) DeleteObject(context.Context, string, string) error {
	return nil
}

func (f *fakeStorage) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	data, ok := f.objects[bucket+"/"+key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type stubConverter struct {
	calls int
	pdf   []byte
	err   error
}

func (s *stubConverter) OfficeToPDF(context.Context, string, []byte) ([]byte, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.pdf, nil
}

func officeFile() domain.File {
	return domain.File{
		TenantID:         "t1",
		PublicID:         "mf_office",
		Bucket:           "media",
		ObjectKey:        "tenants/t1/2026/01/doc.docx",
		ContentType:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		OriginalFilename: "doc.docx",
		SizeBytes:        2048,
	}
}

func TestIsOfficeConvertible(t *testing.T) {
	cases := []struct {
		contentType string
		filename    string
		want        bool
	}{
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "doc.docx", true},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "sheet.xlsx", true},
		{"application/vnd.ms-excel", "legacy.xls", true},
		{"application/msword", "legacy.doc", true},
		{"application/octet-stream", "doc.docx", true},
		{"application/octet-stream", "slides.pptx", true},
		{"application/octet-stream", "notes.txt", false},
		{"application/pdf", "doc.pdf", false},
		{"image/png", "image.png", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := isOfficeConvertible(tc.contentType, tc.filename); got != tc.want {
			t.Errorf("isOfficeConvertible(%q, %q) = %v, want %v", tc.contentType, tc.filename, got, tc.want)
		}
	}
}

func TestPreviewPDFPassesPDFThrough(t *testing.T) {
	file := officeFile()
	file.ContentType = "application/pdf"
	file.ObjectKey = "tenants/t1/doc.pdf"
	pdf := []byte("%PDF-1.7 original")
	store := newFakeStorage(map[string][]byte{"media/" + file.ObjectKey: pdf})
	svc := NewMediaService(config.Config{}, nil, store)

	got, err := svc.PreviewPDF(context.Background(), file)
	if err != nil {
		t.Fatalf("PreviewPDF: %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Fatalf("PreviewPDF = %q, want the stored PDF", got)
	}
}

func TestPreviewPDFConvertsAndCaches(t *testing.T) {
	file := officeFile()
	source := []byte("ooxml-bytes")
	store := newFakeStorage(map[string][]byte{"media/" + file.ObjectKey: source})
	converter := &stubConverter{pdf: []byte("%PDF-1.7 converted")}
	svc := NewMediaService(config.Config{}, nil, store, WithDocumentConverter(converter))

	first, err := svc.PreviewPDF(context.Background(), file)
	if err != nil {
		t.Fatalf("PreviewPDF(first): %v", err)
	}
	if !bytes.Equal(first, converter.pdf) {
		t.Fatalf("first preview = %q, want converted PDF", first)
	}
	if converter.calls != 1 {
		t.Fatalf("converter calls = %d, want 1", converter.calls)
	}
	if _, ok := store.objects["media/"+previewObjectKey(file)]; !ok {
		t.Fatal("converted PDF was not cached in storage")
	}

	second, err := svc.PreviewPDF(context.Background(), file)
	if err != nil {
		t.Fatalf("PreviewPDF(second): %v", err)
	}
	if !bytes.Equal(second, converter.pdf) {
		t.Fatalf("second preview = %q, want cached PDF", second)
	}
	if converter.calls != 1 {
		t.Fatalf("converter calls after cache hit = %d, want 1", converter.calls)
	}
}

func TestPreviewPDFRejectsUnsupportedType(t *testing.T) {
	file := officeFile()
	file.ContentType = "text/plain"
	file.OriginalFilename = "notes.txt"
	store := newFakeStorage(map[string][]byte{"media/" + file.ObjectKey: []byte("hello")})
	svc := NewMediaService(config.Config{}, nil, store, WithDocumentConverter(&stubConverter{}))

	if _, err := svc.PreviewPDF(context.Background(), file); !errors.Is(err, ErrPreviewUnsupported) {
		t.Fatalf("PreviewPDF error = %v, want ErrPreviewUnsupported", err)
	}
}

func TestPreviewPDFUnavailableWithoutConverter(t *testing.T) {
	file := officeFile()
	store := newFakeStorage(map[string][]byte{"media/" + file.ObjectKey: []byte("ooxml")})
	svc := NewMediaService(config.Config{}, nil, store)

	if _, err := svc.PreviewPDF(context.Background(), file); !errors.Is(err, ErrPreviewUnavailable) {
		t.Fatalf("PreviewPDF error = %v, want ErrPreviewUnavailable", err)
	}
}

func TestPreviewPDFRejectsOversizedOfficeFile(t *testing.T) {
	file := officeFile()
	file.SizeBytes = 2 * 1024 * 1024
	store := newFakeStorage(map[string][]byte{"media/" + file.ObjectKey: []byte("ooxml")})
	svc := NewMediaService(config.Config{PreviewMaxSizeMB: 1}, nil, store, WithDocumentConverter(&stubConverter{}))

	if _, err := svc.PreviewPDF(context.Background(), file); !errors.Is(err, ErrPreviewTooLarge) {
		t.Fatalf("PreviewPDF error = %v, want ErrPreviewTooLarge", err)
	}
}

func TestPreviewObjectKeyIsStablePerFile(t *testing.T) {
	file := officeFile()
	want := "derived/t1/mf_office/v1.pdf"
	if got := previewObjectKey(file); got != want {
		t.Fatalf("previewObjectKey() = %q, want %q", got, want)
	}
}

func TestMaxStreamBytesDefaultsToTwoMegabytes(t *testing.T) {
	svc := NewMediaService(config.Config{}, nil, nil)
	if got := svc.MaxStreamBytes(); got != 2*1024*1024 {
		t.Fatalf("MaxStreamBytes() = %d, want %d", got, 2*1024*1024)
	}
}

func TestMaxStreamBytesUsesConfiguredLimit(t *testing.T) {
	svc := NewMediaService(config.Config{StreamMaxSizeMB: 64}, nil, nil)
	if got := svc.MaxStreamBytes(); got != 64*1024*1024 {
		t.Fatalf("MaxStreamBytes() = %d, want %d", got, 64*1024*1024)
	}
}

func TestSupportsBrowserPresignWithoutProvider(t *testing.T) {
	svc := NewMediaService(config.Config{}, nil, nil)
	if svc.SupportsBrowserPresign() {
		t.Fatal("SupportsBrowserPresign() = true, want false without a provider")
	}
}

func TestCanWarmPreview(t *testing.T) {
	office := officeFile()
	converter := &stubConverter{pdf: []byte("%PDF")}

	cases := []struct {
		name      string
		converter DocumentConverter
		file      domain.File
		cfg       config.Config
		want      bool
	}{
		{"office with converter", converter, office, config.Config{}, true},
		{"without converter", nil, office, config.Config{}, false},
		{"non office", converter, domain.File{ContentType: "image/png", OriginalFilename: "a.png", SizeBytes: 10}, config.Config{}, false},
		{"oversized", converter, domain.File{ContentType: office.ContentType, OriginalFilename: office.OriginalFilename, SizeBytes: 2 * 1024 * 1024}, config.Config{PreviewMaxSizeMB: 1}, false},
	}
	for _, tc := range cases {
		svc := NewMediaService(tc.cfg, nil, nil, WithDocumentConverter(tc.converter))
		if got := svc.canWarmPreview(tc.file); got != tc.want {
			t.Errorf("%s: canWarmPreview() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
