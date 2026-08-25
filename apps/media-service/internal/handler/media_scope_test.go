package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/media-service/internal/domain"
)

func TestMediaScopeRequiresTenantAndActiveOrganization(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		wantOK  bool
	}{
		{name: "missing tenant", headers: map[string]string{"X-Org-Id": "org-1"}},
		{name: "missing active organization", headers: map[string]string{"X-Tenant-Id": "tenant-1"}},
		{name: "complete scope", headers: map[string]string{"X-Tenant-Id": "tenant-1", "X-Org-Id": "org-1"}, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/media/mf-1", nil)
			for key, value := range tt.headers {
				r.Header.Set(key, value)
			}
			_, ok := mediaScope(r)
			if ok != tt.wantOK {
				t.Fatalf("mediaScope ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestPublicFileJSONDoesNotExposeStorageCoordinates(t *testing.T) {
	file := domain.File{
		ID: "internal-id", PublicID: "public-id", Module: "iam", OriginalFilename: "avatar.png",
		ContentType: "image/png", SizeBytes: 42, Status: domain.StatusTemp, ScanStatus: domain.ScanNotRequired,
		StorageProvider: "garage", Bucket: "private-bucket", ObjectKey: "tenants/t/file", CreatedAt: time.Now().UTC(),
		Visibility: "private",
	}
	got := publicFileJSON(file)
	if _, ok := got["id"]; ok {
		t.Fatal("public media response exposed internal id")
	}
	if _, ok := got["object_key"]; ok {
		t.Fatal("public media response exposed storage object key")
	}
	if got["public_id"] != "public-id" {
		t.Fatalf("public_id = %#v", got["public_id"])
	}
}
