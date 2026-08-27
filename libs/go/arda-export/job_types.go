package ardaexport

import "time"

// JobStatus represents the lifecycle stage of an asynchronous export job.
type JobStatus string

const (
	JobStatusPending    JobStatus = "PENDING"
	JobStatusProcessing JobStatus = "PROCESSING"
	JobStatusCompleted  JobStatus = "COMPLETED"
	JobStatusFailed     JobStatus = "FAILED"
	JobStatusCancelled  JobStatus = "CANCELLED"
)

// ExportJob represents an asynchronous export task in the enterprise system.
type ExportJob struct {
	ID              string       `json:"id"`
	TenantID        string       `json:"tenantId"`
	UserID          string       `json:"userId"`
	ModuleName      string       `json:"moduleName"`
	EntityName      string       `json:"entityName"`
	Format          ExportFormat `json:"format"`
	Status          JobStatus    `json:"status"`
	ProgressPercent int          `json:"progressPercent"`
	TotalRows       int          `json:"totalRows"`
	ProcessedRows   int          `json:"processedRows"`
	FileURL         string       `json:"fileUrl,omitempty"`
	FileSizeBytes   int64        `json:"fileSizeBytes,omitempty"`
	ErrorMessage    string       `json:"errorMessage,omitempty"`
	ExpiresAt       time.Time    `json:"expiresAt"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

// CreateJobRequest defines parameters for initiating a background export.
type CreateJobRequest struct {
	ModuleName      string          `json:"moduleName"`
	EntityName      string          `json:"entityName"`
	Format          ExportFormat    `json:"format"`
	Filters         map[string]any  `json:"filters"`
	SelectedColumns []string        `json:"selectedColumns"`
	EstimatedCount  int             `json:"estimatedCount"`
}

// JobProgressCallback is invoked periodically during long export operations.
type JobProgressCallback func(processed int, total int)
