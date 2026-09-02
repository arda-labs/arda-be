from sqlalchemy import (
    ARRAY, BigInteger, Column, DateTime, ForeignKey, Index, Integer, MetaData,
    String, Table, Text, func,
)
from sqlalchemy.dialects.postgresql import JSONB, UUID

meta = MetaData(schema="public")

knowledge_sources = Table(
    "ai_knowledge_sources", meta,
    Column("id", BigInteger, primary_key=True, autoincrement=True),
    Column("tenant_id", String, nullable=True),
    Column("title", Text, nullable=False),
    Column("description", Text, nullable=True),
    Column("source_type", Text, nullable=False, server_default=func.text("'docs'")),
    Column("scope", Text, nullable=False, server_default=func.text("'tenant'")),
    Column("classification", Text, nullable=False, server_default=func.text("'internal'")),
    Column("language", Text, server_default=func.text("'vi'")),
    Column("tags", ARRAY(String), server_default=func.text("'{}'")),
    Column("owner_id", Text, nullable=True),
    Column("effective_from", DateTime(timezone=True), nullable=True),
    Column("effective_to", DateTime(timezone=True), nullable=True),
    Column("active_version_id", BigInteger, nullable=True),
    Column("deleted_at", DateTime(timezone=True), nullable=True),
    Column("created_by", Text, nullable=True),
    Column("created_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
)

knowledge_source_versions = Table(
    "ai_knowledge_source_versions", meta,
    Column("id", BigInteger, primary_key=True, autoincrement=True),
    Column("source_id", BigInteger, ForeignKey("ai_knowledge_sources.id", ondelete="CASCADE"), nullable=False),
    Column("version", Text, nullable=False),
    Column("status", Text, nullable=False, server_default=func.text("'DRAFT'")),
    Column("content_type", Text, nullable=False, server_default=func.text("'markdown'")),
    Column("content_url", Text, nullable=True),
    Column("chunker_version", Text, nullable=True),
    Column("chunk_size", Integer, nullable=True),
    Column("chunk_overlap", Integer, nullable=True),
    Column("content_hash", Text, nullable=True),
    Column("status_history", JSONB, server_default=func.text("'[]'::jsonb")),
    Column("created_by", Text, nullable=True),
    Column("created_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
)

ingestion_jobs = Table(
    "ai_ingestion_jobs", meta,
    Column("id", UUID, primary_key=True, server_default=func.gen_random_uuid()),
    Column("source_version_id", BigInteger, ForeignKey("ai_knowledge_source_versions.id", ondelete="CASCADE"), nullable=False),
    Column("status", Text, nullable=False, server_default=func.text("'pending'")),
    Column("locked_by", Text, nullable=True),
    Column("locked_at", DateTime(timezone=True), nullable=True),
    Column("attempts", Integer, server_default=func.text("0")),
    Column("max_attempts", Integer, server_default=func.text("3")),
    Column("error_message", Text, nullable=True),
    Column("total_chunks", Integer, server_default=func.text("0")),
    Column("embedded_chunks", Integer, server_default=func.text("0")),
    Column("next_retry_at", DateTime(timezone=True), nullable=True),
    Column("created_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
)

# Indexes
Index("idx_ingestion_jobs_claim", ingestion_jobs.c.status, ingestion_jobs.c.created_at,
      postgresql_where=ingestion_jobs.c.status == "pending")