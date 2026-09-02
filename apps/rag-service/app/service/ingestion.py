"""Chunk + embed + persist pipeline for one ingestion job (spec §4.2)."""

import hashlib
import logging

from sqlalchemy import Engine, text

from app.db.queue import Job, heartbeat
from app.rag.chunker import chunk_markdown

logger = logging.getLogger(__name__)


def process_job(engine: Engine, job: Job, embedder, batch_size: int = 16) -> None:
    """Run the ingestion pipeline for a claimed job.

    Raises on any error (e.g. EmbeddingError) — the worker loop decides
    whether to requeue or fail the job.
    """
    with engine.begin() as conn:
        ver = conn.execute(
            text(
                "SELECT content, chunker_version, chunk_size, chunk_overlap"
                " FROM ai_knowledge_source_versions WHERE id = :vid"
            ),
            {"vid": job.source_version_id},
        ).mappings().one()
        content, chunker_version = ver["content"], ver["chunker_version"] or "1"
        chunk_size, chunk_overlap = ver["chunk_size"] or 512, ver["chunk_overlap"] or 64

        chunks = chunk_markdown(
            content, chunk_size=chunk_size, chunk_overlap=chunk_overlap,
            chunker_version=chunker_version,
        )

        inserted, existing = 0, 0
        for i, chunk in enumerate(chunks):
            chunk_id = hashlib.sha256(
                f"{job.source_version_id}:{i}:{chunk.content_hash}:{chunker_version}".encode()
            ).hexdigest()
            result = conn.execute(
                text(
                    "INSERT INTO ai_knowledge_chunks"
                    " (source_version_id, chunk_index, heading, content, chunk_id, content_hash)"
                    " VALUES (:vid, :idx, :heading, :content, :cid, :ch)"
                    " ON CONFLICT (chunk_id) DO NOTHING"
                ),
                {
                    "vid": job.source_version_id,
                    "idx": i,
                    "heading": chunk.heading,
                    "content": chunk.content,
                    "cid": chunk_id,
                    "ch": chunk.content_hash,
                },
            )
            inserted += result.rowcount
            existing += 0 if result.rowcount else 1
        logger.info(
            "job %s: %d chunks (%d inserted, %d existing)",
            job.id, len(chunks), inserted, existing,
        )

        conn.execute(
            text(
                "UPDATE ai_ingestion_jobs SET total_chunks = :n, updated_at = now()"
                " WHERE id = :job"
            ),
            {"n": len(chunks), "job": job.id},
        )

    # Resume: chunks missing an embedding (e.g. previous attempt crashed mid-batch)
    with engine.connect() as conn:
        missing = conn.execute(
            text(
                "SELECT chunk_id FROM ai_knowledge_chunks"
                " WHERE source_version_id = :vid AND embedding IS NULL"
            ),
            {"vid": job.source_version_id},
        ).scalars().all()

    if missing:
        embedder_model = embedder.model
        for i in range(0, len(missing), batch_size):
            ids = missing[i:i + batch_size]
            with engine.begin() as conn:
                rows = conn.execute(
                    text(
                        "SELECT chunk_id, content FROM ai_knowledge_chunks"
                        " WHERE chunk_id = ANY(:ids)"
                    ),
                    {"ids": ids},
                ).mappings().all()
            texts = [r["content"] for r in rows]
            if not texts:
                continue
            vectors = embedder.embed(texts)
            with engine.begin() as conn:
                for row, vector in zip(rows, vectors):
                    conn.execute(
                        text(
                            "UPDATE ai_knowledge_chunks"
                            " SET embedding = :emb, embedding_model = :model,"
                            "     embedding_dimensions = :dims"
                            " WHERE chunk_id = :cid"
                        ),
                        {
                            "emb": vector,
                            "model": embedder_model,
                            "dims": embedder.dimensions,
                            "cid": row["chunk_id"],
                        },
                    )
                conn.execute(
                    text(
                        "UPDATE ai_ingestion_jobs"
                        " SET embedded_chunks = embedded_chunks + :n, updated_at = now()"
                        " WHERE id = :job"
                    ),
                    {"n": len(ids), "job": job.id},
                )
                heartbeat(conn, job.id)
        logger.info("job %s: embedded %d chunk(s)", job.id, len(missing))