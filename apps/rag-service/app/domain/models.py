import uuid
from datetime import datetime
from pydantic import BaseModel, Field, field_validator


class SourceCreate(BaseModel):
    title: str = Field(min_length=1, max_length=500)
    description: str | None = None
    source_type: str = "docs"  # docs | admin | url
    scope: str = "tenant"      # tenant | global | system
    classification: str = "internal"
    language: str = "vi"
    tags: list[str] = []
    owner_id: str | None = None
    effective_from: datetime | None = None  # ISO-8601
    effective_to: datetime | None = None


class ChunkerConfig(BaseModel):
    strategy: str = "heading"
    chunk_size: int = 512
    chunk_overlap: int = 64
    chunker_version: str = "1"

    @field_validator("chunk_size")
    @classmethod
    def _chunk_size_ge_1(cls, v: int) -> int:
        if v < 1:
            raise ValueError("chunk_size must be >= 1")
        return v

    @field_validator("chunk_overlap")
    @classmethod
    def _overlap_lt_chunk_size(cls, v: int, info) -> int:
        cs = info.data.get("chunk_size", 512)
        if not 0 <= v < cs:
            raise ValueError(f"chunk_overlap must satisfy 0 <= chunk_overlap < chunk_size ({cs})")
        return v


class VersionCreate(BaseModel):
    version: str = Field(min_length=1, max_length=128)
    content_type: str = "markdown"  # markdown | url | file
    content: str | None = None      # when markdown
    content_url: str | None = None  # when url/file
    chunker_config: ChunkerConfig | None = None


class ReviewRequest(BaseModel):
    decision: str  # approve | reject
    reason: str | None = None


class SourceOut(BaseModel):
    id: int
    tenant_id: str | None = None
    title: str
    description: str | None = None
    source_type: str
    scope: str
    classification: str
    language: str | None = None
    tags: list[str] = []
    owner_id: str | None = None
    effective_from: datetime | None = None
    effective_to: datetime | None = None
    active_version_id: int | None = None
    deleted_at: datetime | None = None
    created_by: str | None = None
    created_at: datetime | None = None
    updated_at: datetime | None = None
    # Computed from active version
    status: str | None = None
    version: str | None = None


class VersionOut(BaseModel):
    id: int
    source_id: int
    version: str
    status: str
    content_type: str
    content: str | None = None
    content_url: str | None = None
    chunker_version: str | None = None
    chunk_size: int | None = None
    chunk_overlap: int | None = None
    content_hash: str | None = None
    status_history: list[dict] = []
    created_by: str | None = None
    created_at: datetime | None = None
    updated_at: datetime | None = None


class PublishResult(BaseModel):
    job_id: str
    version_id: int
    status: str


class JobOut(BaseModel):
    id: str
    source_version_id: int
    status: str
    locked_by: str | None = None
    locked_at: datetime | None = None
    attempts: int = 0
    max_attempts: int = 3
    error_message: str | None = None
    total_chunks: int = 0
    embedded_chunks: int = 0
    next_retry_at: datetime | None = None
    created_at: datetime | None = None
    updated_at: datetime | None = None


class FeedbackCreate(BaseModel):
    run_id: str
    helpful: bool
    comment: str | None = None

    @field_validator("run_id")
    @classmethod
    def _run_id_uuid(cls, v: str) -> str:
        uuid.UUID(v)  # ValueError -> FastAPI 422
        return v


class FeedbackOut(BaseModel):
    id: str
    run_id: str
    helpful: bool
    comment: str | None = None
    created_at: datetime | None = None


class QueryRequest(BaseModel):
    """POST /api/rag/query body — identity fields never allowed here."""
    query: str = Field(min_length=1, max_length=512)
    top_k: int = Field(ge=1, le=10)

    @field_validator("query")
    @classmethod
    def _query_not_whitespace(cls, v: str) -> str:
        if not v.strip():
            raise ValueError("query must not be empty or whitespace")
        return v


class QueryHitOut(BaseModel):
    source_id: int
    source_version_id: int
    version: str
    title: str
    heading: str
    content: str
    score: float          # RRF fused score (ruling 2 — never raw leg scores)
    citation: str         # f"[{source_id}:{heading}]"


class QueryResponse(BaseModel):
    run_id: str
    hits: list[QueryHitOut]
    latency_ms: int
    rewritten: bool = False          # P3.4 — always false in Phase 1
    retrieved_count: int             # candidates BEFORE rerank
    reranked_count: int              # len(hits) after rerank + clamp


class ChunkPreviewOut(BaseModel):
    index: int
    heading: str
    content: str
    content_hash: str
    word_count: int
    char_count: int


class ChunkPreviewRequest(BaseModel):
    content: str
    chunker_config: ChunkerConfig | None = None


class ChunkPreviewResponse(BaseModel):
    total_chunks: int
    extracted_text: str | None = None
    chunks: list[ChunkPreviewOut]
