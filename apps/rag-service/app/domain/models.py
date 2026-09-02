from datetime import datetime
from pydantic import BaseModel, Field


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