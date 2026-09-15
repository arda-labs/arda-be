package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const testSecret = "01234567890123456789012345678901"

type fakeAI struct {
	mu           sync.Mutex
	sources      map[int64]*apiSource
	versions     map[int64][]apiVersion
	jobs         map[string]string
	nextSource   int64
	nextVersion  int64
	nextJob      int
	versionCalls int
}

func newFakeAI() *fakeAI {
	return &fakeAI{
		sources:  map[int64]*apiSource{},
		versions: map[int64][]apiVersion{},
		jobs:     map[string]string{},
	}
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// handler mirrors the real /api/rag/* surface closely enough for the pipeline:
// identity checks, create source/version, review, publish (job completes
// immediately) and job polling.
func (f *fakeAI) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := identity.Verify(r.Header.Get(identity.MetadataKey), testSecret, "ai-service", time.Now()); err != nil {
			t.Errorf("unsigned or invalid workload identity: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Auth-Checked") != "true" ||
			r.Header.Get("X-User-Id") != "corpus-bot" ||
			r.Header.Get("X-Tenant-Id") != "tenant-1" ||
			!strings.Contains(r.Header.Get("X-Permissions"), "ai.knowledge.manage") {
			t.Errorf("delegated identity headers missing: %+v", r.Header)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.URL.Path == "/api/rag/sources" && r.Method == http.MethodGet:
			list := make([]apiSource, 0, len(f.sources))
			for _, source := range f.sources {
				list = append(list, *source)
			}
			_ = json.NewEncoder(w).Encode(list)

		case r.URL.Path == "/api/rag/sources" && r.Method == http.MethodPost:
			var payload struct {
				Title   string   `json:"title"`
				Scope   string   `json:"scope"`
				Tags    []string `json:"tags"`
				OwnerID *string  `json:"owner_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			f.nextSource++
			source := &apiSource{
				ID:      f.nextSource,
				Title:   payload.Title,
				Scope:   payload.Scope,
				Tags:    payload.Tags,
				OwnerID: payload.OwnerID,
			}
			f.sources[source.ID] = source
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(source)

		case len(parts) == 5 && parts[2] == "sources" && parts[4] == "versions" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.versions[int64(atoi(t, parts[3]))])

		case len(parts) == 5 && parts[2] == "sources" && parts[4] == "versions" && r.Method == http.MethodPost:
			sourceID := int64(atoi(t, parts[3]))
			var payload struct {
				Version string `json:"version"`
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			f.nextVersion++
			version := apiVersion{
				ID:          f.nextVersion,
				Status:      "DRAFT",
				ContentHash: strPtr(hashContent(payload.Content)),
			}
			f.versions[sourceID] = append(f.versions[sourceID], version)
			f.versionCalls++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(version)

		case len(parts) == 7 && parts[2] == "sources" && parts[4] == "versions" && parts[6] == "review" && r.Method == http.MethodPost:
			sourceID := int64(atoi(t, parts[3]))
			versionID := int64(atoi(t, parts[5]))
			for i := range f.versions[sourceID] {
				if f.versions[sourceID][i].ID == versionID {
					f.versions[sourceID][i].Status = "APPROVED"
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)

		case len(parts) == 7 && parts[2] == "sources" && parts[4] == "versions" && parts[6] == "publish" && r.Method == http.MethodPost:
			sourceID := int64(atoi(t, parts[3]))
			versionID := int64(atoi(t, parts[5]))
			for i := range f.versions[sourceID] {
				if f.versions[sourceID][i].ID == versionID {
					f.versions[sourceID][i].Status = "PUBLISHED"
					f.nextJob++
					jobID := fmt.Sprintf("job-%d", f.nextJob)
					f.jobs[jobID] = "completed"
					_ = json.NewEncoder(w).Encode(map[string]string{"job_id": jobID, "status": "pending"})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)

		case len(parts) == 4 && parts[2] == "jobs" && r.Method == http.MethodGet:
			jobID := parts[3]
			status, ok := f.jobs[jobID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(apiJob{ID: jobID, Status: status})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func atoi(t *testing.T, raw string) int {
	t.Helper()
	value := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			t.Fatalf("non-numeric path segment %q", raw)
		}
		value = value*10 + int(ch-'0')
	}
	return value
}

func strPtr(value string) *string { return &value }

// seedCorpus creates a temp manifest directory with two markdown files.
func seedCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A\n\nNội dung tài liệu A."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "b.md"), []byte("# B\n\nNội dung tài liệu B."), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "corpus:\n" +
		"  - name: test-corpus\n" +
		"    sources:\n" +
		"      - glob: docs/*.md\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "manifest.yaml")
}

func newTestClient(server *httptest.Server) *Client {
	client := NewClient(server.URL, "corpus-bot", "tenant-1", "ai.knowledge.manage", true, server.Client())
	client.Secret = testSecret
	return client
}

func TestSyncCreatesThenSkips(t *testing.T) {
	fake := newFakeAI()
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	manifestPath := seedCorpus(t)
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	plans, err := Collect(manifestPath, manifest)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	client := newTestClient(server)

	report, err := Sync(context.Background(), client, plans, Options{})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(report.Created) != 2 || len(report.Failures) != 0 {
		t.Fatalf("first sync report = %+v", report)
	}
	fake.mu.Lock()
	versionsAfterFirst := fake.versionCalls
	tags := fake.sources[1].Tags
	owner := fake.sources[1].OwnerID
	fake.mu.Unlock()
	if versionsAfterFirst != 2 {
		t.Fatalf("expected 2 version creations, got %d", versionsAfterFirst)
	}
	if !contains(tags, DocsAsCodeTag) {
		t.Fatalf("created source must carry the docs-as-code marker: %v", tags)
	}
	if owner == nil || *owner != DefaultOwner {
		t.Fatalf("created source owner = %v, want %s", owner, DefaultOwner)
	}

	report2, err := Sync(context.Background(), client, plans, Options{})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(report2.Skipped) != 2 || len(report2.Created) != 0 || len(report2.Updated) != 0 {
		t.Fatalf("second sync report = %+v", report2)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.versionCalls != versionsAfterFirst {
		t.Fatal("unchanged content must not create a new version")
	}
}

func TestSyncContentChangeCreatesNewVersion(t *testing.T) {
	fake := newFakeAI()
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	manifestPath := seedCorpus(t)
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := Collect(manifestPath, manifest)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(server)
	if _, err := Sync(context.Background(), client, plans, Options{}); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	docs := filepath.Join(filepath.Dir(manifestPath), "docs")
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A\n\nNội dung đã cập nhật."), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := Collect(manifestPath, manifest)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Sync(context.Background(), client, changed, Options{})
	if err != nil {
		t.Fatalf("changed sync: %v", err)
	}
	if len(report.Updated) != 1 || len(report.Skipped) != 1 {
		t.Fatalf("changed sync report = %+v", report)
	}
}

func TestSyncFailsClosedOnSecretPattern(t *testing.T) {
	fake := newFakeAI()
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "leak.md"), []byte("password: super-secret-value-123"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "corpus:\n  - name: bad\n    sources:\n      - glob: leak.md\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plans, err := Collect(filepath.Join(dir, "manifest.yaml"), loaded)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(server)

	_, err = Sync(context.Background(), client, plans, Options{})
	if err == nil {
		t.Fatal("secret pattern must fail closed with an error")
	}
	if fake.versionCalls != 0 {
		t.Fatal("no API call may happen when the scan rejects content")
	}
}

func TestLoadManifestDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte("corpus:\n  - name: docs\n    sources:\n      - glob: docs/*.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Corpus[0]
	if entry.Scope != DefaultScope || entry.Owner != DefaultOwner || entry.Classification != "internal" || entry.Language != "vi" {
		t.Fatalf("defaults not applied: %+v", entry)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
