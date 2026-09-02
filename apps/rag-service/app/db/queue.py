"""Durable Postgres-backed job queue with SKIP LOCKED semantics.

Every function takes a synchronous SQLAlchemy connection (not engine) so
callers control transaction boundaries.
"""

import logging
from dataclasses import dataclass

from sqlalchemy import text

logger = logging.getLogger(__name__)


@dataclass
class Job:
    """A claimed ingestion job returned by claim_job."""

    id: str
    source_version_id: int
    source_id: int
    attempts: int
    max_attempts: int


def claim_job(conn, worker_id: str, lease_sec: int) -> Job | None:
    """Atomically claim the next pending job with SKIP LOCKED.

    Bumps attempts, sets status='processing', and returns the job details.
    Returns None when no pending job is available.
    """
    row = conn.execute(
        text("""
            UPDATE ai_ingestion_jobs AS j
               SET status = 'processing', locked_by = :worker, locked_at = now(),
                   attempts = attempts + 1
             FROM ai_knowledge_source_versions AS v
             WHERE j.id = (
                 SELECT j2.id FROM ai_ingestion_jobs j2
                  WHERE j2.status = 'pending'
                    AND j2.attempts < j2.max_attempts
                    AND (j2.next_retry_at IS NULL OR j2.next_retry_at <= now())
                  ORDER BY j2.created_at ASC
                  LIMIT 1
                  FOR UPDATE SKIP LOCKED
             )
               AND j.source_version_id = v.id
             RETURNING j.id, j.source_version_id, v.source_id,
                       j.attempts, j.max_attempts
        """),
        {"worker": worker_id},
    ).mappings().one_or_none()
    if row is None:
        return None
    return Job(
        id=str(row["id"]),
        source_version_id=row["source_version_id"],
        source_id=row["source_id"],
        attempts=row["attempts"],
        max_attempts=row["max_attempts"],
    )


def heartbeat(conn, job_id: str) -> None:
    """Refresh locked_at so the lease doesn't expire."""
    conn.execute(
        text(
            "UPDATE ai_ingestion_jobs SET locked_at = now()"
            " WHERE id = :job AND status = 'processing'"
        ),
        {"job": job_id},
    )


def complete_job(conn, job_id: str, version_id: int, source_id: int) -> None:
    """Atomic 3-update: job completed + version PUBLISHED + source.active_version_id.

    Raises RuntimeError if any rowcount check fails (the whole transaction rolls back).
    """
    job = conn.execute(
        text(
            "UPDATE ai_ingestion_jobs SET status='completed', updated_at=now()"
            " WHERE id=:job AND status='processing' RETURNING id"
        ),
        {"job": job_id},
    ).fetchone()
    ver = conn.execute(
        text(
            "UPDATE ai_knowledge_source_versions SET status='PUBLISHED', updated_at=now()"
            " WHERE id=:vid AND source_id=:sid AND status='INDEXING' RETURNING id"
        ),
        {"vid": version_id, "sid": source_id},
    ).fetchone()
    src = conn.execute(
        text(
            "UPDATE ai_knowledge_sources SET active_version_id=:vid, updated_at=now()"
            " WHERE id=:sid RETURNING id"
        ),
        {"vid": version_id, "sid": source_id},
    ).fetchone()
    if not (job and ver and src):
        raise RuntimeError("atomic publish failed -- rowcount mismatch")


def fail_or_requeue(
    conn, job_id: str, version_id: int, error: str,
    attempts: int, max_attempts: int,
) -> None:
    """Fail the job (and version) when attempts exhausted, or requeue with backoff."""
    if attempts >= max_attempts:
        conn.execute(
            text(
                "UPDATE ai_ingestion_jobs SET status='failed', error_message=:err, updated_at=now()"
                " WHERE id=:job"
            ),
            {"job": job_id, "err": error},
        )
        conn.execute(
            text(
                "UPDATE ai_knowledge_source_versions SET status='FAILED', updated_at=now()"
                " WHERE id=:vid"
            ),
            {"vid": version_id},
        )
        logger.warning(
            "job %s failed after %d/%d attempts: %s",
            job_id, attempts, max_attempts, error,
        )
    else:
        backoff = {1: 0.5, 2: 2, 3: 8}.get(attempts, 8)
        conn.execute(
            text(
                "UPDATE ai_ingestion_jobs"
                " SET status='pending', error_message=:err,"
                "     next_retry_at=now() + make_interval(secs => :backoff), updated_at=now()"
                " WHERE id=:job"
            ),
            {"job": job_id, "err": error, "backoff": backoff},
        )
        logger.info(
            "job %s requeued with %.1fs backoff (attempt %d/%d)",
            job_id, backoff, attempts, max_attempts,
        )


def reset_expired_leases(conn, lease_sec: int) -> int:
    """Reset jobs whose lease has expired back to pending.

    Returns the number of jobs reset.
    """
    result = conn.execute(
        text("""
            UPDATE ai_ingestion_jobs
               SET status = 'pending', locked_by = NULL, locked_at = NULL,
                   next_retry_at = NULL, updated_at = now()
             WHERE status = 'processing'
               AND locked_at < now() - make_interval(secs => :lease)
        """),
        {"lease": lease_sec},
    )
    n = result.rowcount
    if n:
        logger.info("reset %d expired lease(s)", n)
    return n