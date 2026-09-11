package ardadoc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSnapshotPDFFromHTML(t *testing.T) {
	var uploads, attaches int
	gotenberg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/chromium/convert/html" {
			t.Errorf("gotenberg path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("%PDF-rendered"))
	}))
	defer gotenberg.Close()

	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/media":
			uploads++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":  map[string]any{"public_id": "mf-new"},
				"success": true,
			})
		case "/api/media/files/attach":
			attaches++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":  map[string]any{"attached": 1},
				"success": true,
			})
		default:
			t.Errorf("unexpected media path %q", r.URL.Path)
		}
	}))
	defer media.Close()

	gotClient, err := NewGotenbergClient(gotenberg.URL)
	if err != nil {
		t.Fatalf("NewGotenbergClient: %v", err)
	}
	mediaClient, err := NewMediaUploader(media.URL)
	if err != nil {
		t.Fatalf("NewMediaUploader: %v", err)
	}
	pipeline := NewSnapshotPipeline(gotClient, mediaClient)

	src := http.Header{}
	src.Set("X-Tenant-Id", "t1")
	src.Set("X-Org-Id", "o1")

	publicID, err := pipeline.SnapshotPDFFromHTML(
		context.Background(), src,
		"<h1>{{.Code}}</h1>", map[string]string{"Code": "HD-001"},
		SnapshotOptions{
			Module:     "finance",
			EntityType: "business_case",
			EntityID:   "case-42",
			Filename:   "HD-001.pdf",
		},
	)
	if err != nil {
		t.Fatalf("SnapshotPDFFromHTML: %v", err)
	}
	if publicID != "mf-new" {
		t.Errorf("publicID = %q", publicID)
	}
	if uploads != 1 || attaches != 1 {
		t.Errorf("uploads=%d attaches=%d", uploads, attaches)
	}
}

func TestSnapshotXLSX(t *testing.T) {
	var storedType string
	gotenberg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("gotenberg must not be called for xlsx snapshots")
	}))
	defer gotenberg.Close()

	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/media":
			storedType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"public_id": "mf-x"}, "success": true})
		case "/api/media/files/attach":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"attached": 1}, "success": true})
		}
	}))
	defer media.Close()

	mediaClient, _ := NewMediaUploader(media.URL)
	pipeline := NewSnapshotPipeline(&GotenbergClient{baseURL: gotenberg.URL, httpClient: http.DefaultClient}, mediaClient)

	publicID, err := pipeline.SnapshotXLSX(
		context.Background(), http.Header{},
		buildTestTemplate(t),
		map[string]any{"Total": 9},
		nil,
		SnapshotOptions{Module: "finance", EntityType: "business_case", EntityID: "case-7", Filename: "bc.xlsx"},
	)
	if err != nil {
		t.Fatalf("SnapshotXLSX: %v", err)
	}
	if publicID != "mf-x" {
		t.Errorf("publicID = %q", publicID)
	}
	if !strings.Contains(storedType, "multipart/form-data") {
		t.Errorf("upload content type = %q", storedType)
	}
}

func TestSnapshotOptionsValidation(t *testing.T) {
	pipeline := NewSnapshotPipeline(nil, nil)
	cases := []SnapshotOptions{
		{EntityType: "business_case", EntityID: "c", Filename: "f.pdf"},
		{Module: "loan", EntityID: "c", Filename: "f.pdf"},
		{Module: "loan", EntityType: "business_case", Filename: "f.pdf"},
		{Module: "loan", EntityType: "business_case", EntityID: "c"},
	}
	for _, opt := range cases {
		if _, err := pipeline.SnapshotPDFFromHTML(context.Background(), nil, "x", nil, opt); err == nil {
			t.Errorf("expected validation error for %+v", opt)
		}
	}
}
