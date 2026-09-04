package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type fakeConnectorStore struct {
	fakeSettingsStore
	connectors []repository.DataConnector
}

func (s *fakeConnectorStore) ListConnectors(_ context.Context, tenantID string) ([]repository.DataConnector, error) {
	if len(s.connectors) == 0 {
		s.connectors = []repository.DataConnector{
			{
				ID:           "conn-1",
				TenantID:     tenantID,
				Name:         "Google Drive Seed",
				Provider:     "google_drive",
				TargetSource: "HR Manual",
				SyncSchedule: "Hourly",
				Status:       "synced",
				LastSyncAt:   time.Now(),
				DocCount:     10,
				TotalChunks:  100,
			},
		}
	}
	return s.connectors, nil
}

func (s *fakeConnectorStore) CreateConnector(_ context.Context, conn repository.DataConnector) (*repository.DataConnector, error) {
	conn.ID = "conn-created"
	conn.CreatedAt = time.Now()
	conn.UpdatedAt = time.Now()
	s.connectors = append(s.connectors, conn)
	return &conn, nil
}

func (s *fakeConnectorStore) DeleteConnector(_ context.Context, _ string, id string) error {
	var filtered []repository.DataConnector
	for _, c := range s.connectors {
		if c.ID != id {
			filtered = append(filtered, c)
		}
	}
	s.connectors = filtered
	return nil
}

func (s *fakeConnectorStore) SyncConnector(_ context.Context, _ string, id string) (*repository.DataConnector, error) {
	for i, c := range s.connectors {
		if c.ID == id {
			s.connectors[i].LastSyncAt = time.Now()
			s.connectors[i].DocCount += 1
			s.connectors[i].TotalChunks += 12
			return &s.connectors[i], nil
		}
	}
	res := repository.DataConnector{
		ID:          id,
		Name:        "Synced",
		Status:      "synced",
		LastSyncAt:  time.Now(),
		DocCount:    1,
		TotalChunks: 12,
	}
	return &res, nil
}

func TestConnectorHandlers(t *testing.T) {
	store := &fakeConnectorStore{}
	router := NewRouterWithOptions(store, nil, RouterOptions{})

	setAuth := func(req *http.Request) {
		req.Header.Set("X-Auth-Checked", "true")
		req.Header.Set("X-Tenant-Id", "tenant-test")
		req.Header.Set("X-User-Id", "usr-1")
	}

	// 1. List connectors
	req := httptest.NewRequest(http.MethodGet, "/api/rag/connectors", nil)
	setAuth(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Google Drive Seed") {
		t.Errorf("expected body to contain connector name, got %s", rec.Body.String())
	}

	// 2. Create connector
	createBody := `{"name":"S3 Sales Docs","provider":"s3_bucket","targetSource":"Sales S3","syncSchedule":"Daily"}`
	req = httptest.NewRequest(http.MethodPost, "/api/rag/connectors", strings.NewReader(createBody))
	setAuth(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "S3 Sales Docs") {
		t.Errorf("expected body to contain created connector, got %s", rec.Body.String())
	}

	// 3. Sync connector
	req = httptest.NewRequest(http.MethodPost, "/api/rag/connectors/conn-1/sync", nil)
	setAuth(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"docCount":11`) {
		t.Errorf("expected docCount incremented to 11, got %s", rec.Body.String())
	}

	// 4. Delete connector
	req = httptest.NewRequest(http.MethodDelete, "/api/rag/connectors/conn-1", nil)
	setAuth(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
