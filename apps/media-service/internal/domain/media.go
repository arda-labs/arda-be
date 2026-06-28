package domain

import "time"

const (
	StatusPendingUpload = "pending_upload"
	StatusUploaded      = "uploaded"
	StatusScanPending   = "scan_pending"
	StatusReady         = "ready"
	StatusQuarantined   = "quarantined"
	StatusDeleted       = "deleted"
	StatusTemp          = "temp"
	StatusAttached      = "attached"

	ScanNotRequired = "not_required"
	ScanPending     = "pending"

	DefaultTenantID = "default"
)

type File struct {
	ID               string     `json:"id"`
	PublicID         string     `json:"public_id"`
	TenantID         string     `json:"tenant_id"`
	OrgID            string     `json:"org_id,omitempty"`
	OwnerUserID      string     `json:"owner_user_id,omitempty"`
	Module           string     `json:"module"`
	EntityType       string     `json:"entity_type,omitempty"`
	EntityID         string     `json:"entity_id,omitempty"`
	OriginalFilename string     `json:"original_filename"`
	ContentType      string     `json:"content_type"`
	Extension        string     `json:"extension,omitempty"`
	SizeBytes        int64      `json:"size_bytes"`
	ChecksumSHA256   string     `json:"checksum_sha256,omitempty"`
	Status           string     `json:"status"`
	ScanStatus       string     `json:"scan_status"`
	StorageProvider  string     `json:"storage_provider"`
	Bucket           string     `json:"bucket"`
	ObjectKey        string     `json:"object_key"`
	StorageClass     string     `json:"storage_class"`
	VersionID        string     `json:"version_id"`
	Visibility       string     `json:"visibility"`
	CreatedBy        string     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UploadedAt       *time.Time `json:"uploaded_at,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type UploadSession struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	FileID    string    `json:"file_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
}

type InitUploadRequest struct {
	TenantID         string `json:"tenant_id"`
	OrgID            string `json:"org_id"`
	OwnerUserID      string `json:"owner_user_id"`
	Module           string `json:"module"`
	EntityType       string `json:"entity_type"`
	EntityID         string `json:"entity_id"`
	OriginalFilename string `json:"original_filename"`
	ContentType      string `json:"content_type"`
	SizeBytes        int64  `json:"size_bytes"`
	ChecksumSHA256   string `json:"checksum_sha256"`
	StorageClass     string `json:"storage_class"`
	Visibility       string `json:"visibility"`
	CreatedBy        string `json:"created_by"`
}

type InitUploadResponse struct {
	File      File      `json:"file"`
	Upload   UploadURL `json:"upload"`
	UploadID string    `json:"upload_id"`
}

type UploadURL struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type CompleteUploadResponse struct {
	File File `json:"file"`
}

type DownloadURLResponse struct {
	FileID    string    `json:"file_id"`
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type AttachRequest struct {
	PublicIDs []string `json:"public_ids"`
	OwnerType string   `json:"owner_type"`
	OwnerID   string   `json:"owner_id"`
}

