package knowledge

import "time"

type Source struct {
	ID              int64      `json:"id"`
	TenantID        *string    `json:"tenant_id"`
	Title           string     `json:"title"`
	Description     *string    `json:"description"`
	SourceType      string     `json:"source_type"`
	Scope           string     `json:"scope"`
	Classification  string     `json:"classification"`
	Language        *string    `json:"language"`
	Tags            []string   `json:"tags"`
	OwnerID         *string    `json:"owner_id"`
	EffectiveFrom   *time.Time `json:"effective_from"`
	EffectiveTo     *time.Time `json:"effective_to"`
	ActiveVersionID *int64     `json:"active_version_id"`
	DeletedAt       *time.Time `json:"deleted_at"`
	CreatedBy       *string    `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Status          *string    `json:"status"`
	Version         *string    `json:"version"`
}

type SourceCreate struct {
	Title          string     `json:"title"`
	Description    *string    `json:"description"`
	SourceType     string     `json:"source_type"`
	Scope          string     `json:"scope"`
	Classification string     `json:"classification"`
	Language       string     `json:"language"`
	Tags           []string   `json:"tags"`
	OwnerID        *string    `json:"owner_id"`
	EffectiveFrom  *time.Time `json:"effective_from"`
	EffectiveTo    *time.Time `json:"effective_to"`
}

type Version struct {
	ID             int64            `json:"id"`
	SourceID       int64            `json:"source_id"`
	Version        string           `json:"version"`
	Status         string           `json:"status"`
	ContentType    string           `json:"content_type"`
	Content        *string          `json:"content"`
	ContentURL     *string          `json:"content_url"`
	ChunkerVersion *string          `json:"chunker_version"`
	ChunkSize      *int             `json:"chunk_size"`
	ChunkOverlap   *int             `json:"chunk_overlap"`
	ContentHash    *string          `json:"content_hash"`
	StatusHistory  []map[string]any `json:"status_history"`
	CreatedBy      *string          `json:"created_by"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type VersionCreate struct {
	Version       string         `json:"version"`
	ContentType   string         `json:"content_type"`
	Content       *string        `json:"content"`
	ContentURL    *string        `json:"content_url"`
	ChunkerConfig *ChunkerConfig `json:"chunker_config"`
}

type ChunkerConfig struct {
	Strategy       string `json:"strategy,omitempty"`
	ChunkSize      int    `json:"chunk_size,omitempty"`
	ChunkOverlap   int    `json:"chunk_overlap,omitempty"`
	ChunkerVersion string `json:"chunker_version,omitempty"`
}

type ReviewRequest struct {
	Decision string  `json:"decision"`
	Reason   *string `json:"reason"`
}

type PublishResult struct {
	JobID     string `json:"job_id"`
	VersionID int64  `json:"version_id"`
	Status    string `json:"status"`
}

type Job struct {
	ID              string     `json:"id"`
	SourceVersionID int64      `json:"source_version_id"`
	Status          string     `json:"status"`
	LockedBy        *string    `json:"locked_by"`
	LockedAt        *time.Time `json:"locked_at"`
	Attempts        int        `json:"attempts"`
	MaxAttempts     int        `json:"max_attempts"`
	ErrorMessage    *string    `json:"error_message"`
	TotalChunks     int        `json:"total_chunks"`
	EmbeddedChunks  int        `json:"embedded_chunks"`
	NextRetryAt     *time.Time `json:"next_retry_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type QueryRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type QueryHit struct {
	SourceID        int64   `json:"source_id"`
	SourceVersionID int64   `json:"source_version_id"`
	Version         string  `json:"version"`
	Title           string  `json:"title"`
	Heading         string  `json:"heading"`
	Content         string  `json:"content"`
	Score           float64 `json:"score"`
	Citation        string  `json:"citation"`
}

type QueryResponse struct {
	RunID          string     `json:"run_id"`
	Hits           []QueryHit `json:"hits"`
	LatencyMs      int        `json:"latency_ms"`
	Rewritten      bool       `json:"rewritten"`
	RetrievedCount int        `json:"retrieved_count"`
	RerankedCount  int        `json:"reranked_count"`
}

type FeedbackRequest struct {
	RunID   string  `json:"run_id"`
	Helpful bool    `json:"helpful"`
	Comment *string `json:"comment,omitempty"`
}

type FeedbackOut struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	Helpful   bool      `json:"helpful"`
	Comment   *string   `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ChunkPreview struct {
	Index       int    `json:"index"`
	Heading     string `json:"heading"`
	Content     string `json:"content"`
	ContentHash string `json:"content_hash"`
	WordCount   int    `json:"word_count"`
	CharCount   int    `json:"char_count"`
}

type ChunkPreviewRequest struct {
	Content       string         `json:"content"`
	ChunkerConfig *ChunkerConfig `json:"chunker_config,omitempty"`
}

type ChunkPreviewResponse struct {
	TotalChunks   int            `json:"total_chunks"`
	ExtractedText *string        `json:"extracted_text,omitempty"`
	Chunks        []ChunkPreview `json:"chunks"`
}
