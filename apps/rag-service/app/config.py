from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class EmbeddingSettings(BaseSettings):
    """Immutable Phase-1 contract: model + dimensions must match stored vectors."""
    provider: str = "openai-compatible"
    model: str = "@cf/qwen/qwen3-embedding-0.6b"
    dimensions: int = 1024
    api_key_env: str = "CF_WORKERS_AI_API_TOKEN"
    base_url: str = ""   # OpenAI-compatible /embeddings endpoint
    batch_size: int = 16
    concurrency: int = 4


class RerankerSettings(BaseSettings):
    provider: str = "none"            # none | anthropic
    model: str = "claude-haiku-4-5-20251001"
    api_key_env: str = "ANTHROPIC_API_KEY"
    timeout_ms: int = 1500
    candidates: int = 8
    top_n: int = 3


class RetrievalSettings(BaseSettings):
    vector_top_k: int = 8
    fts_top_k: int = 8
    rrf_k: int = 60
    rerank_candidates: int = 8
    final_top_k: int = 3


class QuerySettings(BaseSettings):
    rewrite_enabled: bool = False    # P3.4 placeholder — no LLM rewrite in Phase 1


class WorkerSettings(BaseSettings):
    lease_duration_sec: int = 300     # 5m
    heartbeat_interval_sec: int = 60
    poll_interval_sec: int = 5
    max_attempts: int = 3


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="RAG_", env_nested_delimiter="__")

    service_name: str = "rag-service"
    db_dsn: str = ""                   # required in production
    auth_secret: str = ""              # shared ARDA_SERVICE_AUTH_SECRET
    trusted_sources: list[str] = ["ai-service", "auth-gateway"]
    migrate_on_startup: bool = True
    embedding: EmbeddingSettings = Field(default_factory=EmbeddingSettings)
    reranker: RerankerSettings = Field(default_factory=RerankerSettings)
    retrieval: RetrievalSettings = Field(default_factory=RetrievalSettings)
    query: QuerySettings = Field(default_factory=QuerySettings)   # env RAG_QUERY__REWRITE_ENABLED
    worker: WorkerSettings = Field(default_factory=WorkerSettings)
