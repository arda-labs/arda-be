"""P2.4 integration tests: durable queue (SKIP LOCKED) + ingestion worker.

Requires Postgres (TEST_DATABASE_DSN or the conftest default). Covers:
1. happy path: worker claims job -> chunks -> embeds -> atomic publish
2. failing embedder -> requeue with backoff -> job FAILED + version FAILED after max_attempts
3. expired lease -> reset_expired_leases -> claimable again -> idempotent re-run (no dup chunks)
4. claim_job excludes jobs already processing (SKIP LOCKED) and bumps attempts
"""

import hashlib
import os

from sqlalchemy import create_engine, text

from app.config import EmbeddingSettings, Settings
from app.db import queue
from app.rag.chunker import chunk_markdown
from app.rag.embedder import Embedder, EmbeddingError
from app.worker import run_worker

DSN = os.environ.get(
    "TEST_DATABASE_DSN",
    "postgresql+psycopg://arda_super:123456@192.168.10.201:30432/rag_test",
)

CHUNK_SIZE = 64
CHUNK_OVERLAP = 16
CHUNKER_VERSION = "1"
EMBED_MODEL = "@cf/qwen/qwen3-embedding-0.6b"


class StubEmbedder(Embedder):
    """Deterministic 1024-dim embeddings; raise EmbeddingError when fail=True."""

    def __init__(self, *, fail: bool = False):
        super().__init__(settings=EmbeddingSettings(dimensions=1024, batch_size=16))
        self._fail = fail

    def embed(self, texts):
        if self._fail:
            raise EmbeddingError("stub embedder always fails")
        return [[0.1 * (i % 10 + 1)] * 1024 for i in range(len(texts))]


def _settings() -> Settings:
    return Settings(db_dsn=DSN, migrate_on_startup=False)


def _content() -> str:
    sections = "".join(f"## Section {i}\n\n" + "word " * 40 + "\n\n" for i in range(25))
    return "# Title\n\n" + sections


def _seed_version(client) -> tuple[int, int, str]:
    """Create source + markdown version, approve, publish. Returns (source_id, version_id, job_id)."""
    r = client.post(
        "/api/rag/sources",
        json={"title": "Worker Source", "description": "queue+worker integration", "tags": ["rag"]},
    )
    assert r.status_code == 201, r.text
    source_id = r.json()["id"]

    content = _content()
    r = client.post(
        f"/api/rag/sources/{source_id}/versions",
        json={
            "version": "1.0",
            "content_type": "markdown",
            "content": content,
            "chunker_config": {
                "chunker_version": CHUNKER_VERSION,
                "chunk_size": CHUNK_SIZE,
                "chunk_overlap": CHUNK_OVERLAP,
            },
        },
    )
    assert r.status_code == 201, r.text
    version_id = r.json()["id"]

    r = client.post(
        f"/api/rag/sources/{source_id}/versions/{version_id}/review",
        json={"decision": "approve", "reason": "ok"},
    )
    assert r.status_code == 200, r.text

    r = client.post(f"/api/rag/sources/{source_id}/versions/{version_id}/publish")
    assert r.status_code == 202, r.text
    return source_id, version_id, r.json()["job_id"]


def _expected_chunks() -> list:
    return chunk_markdown(
        _content(), chunk_size=CHUNK_SIZE, chunk_overlap=CHUNK_OVERLAP, chunker_version=CHUNKER_VERSION
    )


def _chunk_id(version_id: int, chunk_index: int, content_hash: str) -> str:
    return hashlib.sha256(
        f"{version_id}:{chunk_index}:{content_hash}:{CHUNKER_VERSION}".encode()
    ).hexdigest()


def test_worker_indexes_and_publishes(client):
    source_id, version_id, job_id = _seed_version(client)
    expected = _expected_chunks()
    assert len(expected) > 16  # multi-batch embedding path

    run_worker(_settings(), once=True, embedder=StubEmbedder())

    # version published + source active
    r = client.get(f"/api/rag/sources/{source_id}/versions/{version_id}")
    assert r.status_code == 200, r.text
    assert r.json()["status"] == "PUBLISHED"
    r = client.get(f"/api/rag/sources/{source_id}")
    assert r.json()["active_version_id"] == version_id

    # job completed with counters
    r = client.get(f"/api/rag/jobs/{job_id}")
    job = r.json()
    assert job["status"] == "completed"
    assert job["total_chunks"] == len(expected)
    assert job["embedded_chunks"] == len(expected)

    # chunks persisted + embedded
    engine = create_engine(DSN, pool_pre_ping=True)
    try:
        with engine.connect() as conn:
            n = conn.execute(
                text("SELECT count(*) FROM ai_knowledge_chunks WHERE source_version_id = :vid"),
                {"vid": version_id},
            ).scalar()
            assert n == len(expected), f"expected {len(expected)} chunks, got {n}"
            nulls = conn.execute(
                text("SELECT count(*) FROM ai_knowledge_chunks WHERE source_version_id = :vid AND embedding IS NULL"),
                {"vid": version_id},
            ).scalar()
            assert nulls == 0
            bad_dims = conn.execute(
                text(
                    "SELECT count(*) FROM ai_knowledge_chunks"
                    " WHERE source_version_id = :vid"
                    "   AND (embedding_dimensions != 1024 OR embedding_model != :model)"
                ),
                {"vid": version_id, "model": EMBED_MODEL},
            ).scalar()
            assert bad_dims == 0
    finally:
        engine.dispose()


def test_worker_fails_job_and_version_after_max_attempts(client):
    source_id, version_id, job_id = _seed_version(client)
    failing = StubEmbedder(fail=True)
    engine = create_engine(DSN, pool_pre_ping=True)
    try:
        for run in range(3):
            run_worker(_settings(), once=True, embedder=failing)
            r = client.get(f"/api/rag/jobs/{job_id}")
            job = r.json()
            assert job["attempts"] == run + 1
            if run < 2:
                # requeued with backoff, not failed yet
                assert job["status"] == "pending"
                assert job["error_message"] == "stub embedder always fails"
                assert job["next_retry_at"] is not None, "worker must schedule a retry"
            # clear the retry gate so the next run claims immediately
            with engine.begin() as conn:
                conn.execute(
                    text("UPDATE ai_ingestion_jobs SET next_retry_at = NULL WHERE id = :job"),
                    {"job": job_id},
                )
    finally:
        engine.dispose()

    # attempts exhausted -> job failed + version FAILED + active_version untouched
    r = client.get(f"/api/rag/jobs/{job_id}")
    assert r.json()["status"] == "failed"
    assert r.json()["attempts"] == 3
    r = client.get(f"/api/rag/sources/{source_id}/versions/{version_id}")
    assert r.json()["status"] == "FAILED"
    r = client.get(f"/api/rag/sources/{source_id}")
    assert r.json()["active_version_id"] is None


def test_expired_lease_reset_and_idempotent_rerun(client):
    source_id, version_id, job_id = _seed_version(client)
    expected = _expected_chunks()
    first = expected[0]

    engine = create_engine(DSN, pool_pre_ping=True)
    try:
        # Simulate a worker that died mid-job: stale lease + one chunk already persisted
        with engine.begin() as conn:
            conn.execute(
                text(
                    "INSERT INTO ai_knowledge_chunks"
                    " (source_version_id, chunk_index, heading, content, chunk_id, content_hash)"
                    " VALUES (:vid, 0, :heading, :content, :cid, :ch)"
                ),
                {
                    "vid": version_id,
                    "heading": first.heading,
                    "content": first.content,
                    "cid": _chunk_id(version_id, 0, first.content_hash),
                    "ch": first.content_hash,
                },
            )
            conn.execute(
                text(
                    "UPDATE ai_ingestion_jobs SET status='processing', locked_by='dead-worker',"
                    " locked_at = now() - interval '1 hour', attempts = 1 WHERE id = :job"
                ),
                {"job": job_id},
            )
            reset = queue.reset_expired_leases(conn, 300)
            assert reset == 1

        # lease expired -> claimable again; re-run resumes and completes without duplicating chunks
        run_worker(_settings(), once=True, embedder=StubEmbedder())

        with engine.connect() as conn:
            n = conn.execute(
                text("SELECT count(*) FROM ai_knowledge_chunks WHERE source_version_id = :vid"),
                {"vid": version_id},
            ).scalar()
            assert n == len(expected), f"duplicate chunks! expected {len(expected)}, got {n}"
            nulls = conn.execute(
                text("SELECT count(*) FROM ai_knowledge_chunks WHERE source_version_id = :vid AND embedding IS NULL"),
                {"vid": version_id},
            ).scalar()
            assert nulls == 0, "resume must embed the chunk persisted before the 'crash'"
    finally:
        engine.dispose()

    r = client.get(f"/api/rag/jobs/{job_id}")
    job = r.json()
    assert job["status"] == "completed"
    assert job["attempts"] == 2  # original claim + re-claim after lease reset
    assert job["total_chunks"] == len(expected)
    r = client.get(f"/api/rag/sources/{source_id}")
    assert r.json()["active_version_id"] == version_id


def test_claim_skips_processing_job_and_bumps_attempts(client):
    source_id, version_id, job_id = _seed_version(client)

    with create_engine(DSN, pool_pre_ping=True).begin() as conn:
        job = queue.claim_job(conn, "worker-a", 300)
        assert job is not None
        assert job.id == job_id
        assert job.source_version_id == version_id
        assert job.source_id == source_id
        assert job.attempts == 1
        assert job.max_attempts == 3

        # already processing -> SKIP LOCKED must not hand it to another worker
        again = queue.claim_job(conn, "worker-b", 300)
        assert again is None
