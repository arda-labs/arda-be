"""Ingestion worker daemon.

CLI: `python -m app.worker` runs the polling loop;
     `python -m app.worker --once` processes at most one job then exits (tests).
"""

import argparse
import logging
import sys
import time
from uuid import uuid4

from sqlalchemy.exc import OperationalError

from app.config import Settings
from app.db import queue
from app.db.engine import get_engine
from app.rag.embedder import Embedder, build_embedder
from app.service import ingestion

logger = logging.getLogger(__name__)


def _run_job(engine, job, embedder) -> None:
    """Process one claimed job. Raises on any job-level error."""
    logger.info(
        "job %s: start (attempt %d/%d)", job.id, job.attempts, job.max_attempts,
    )
    ingestion.process_job(engine, job, embedder)
    with engine.begin() as conn:
        queue.complete_job(conn, job.id, job.source_version_id, job.source_id)
    logger.info("job %s: completed", job.id)


def run_worker(settings: Settings, *, once: bool = False,
               embedder: Embedder | None = None) -> None:
    """Poll for pending jobs, chunk + embed + publish each one."""
    engine = get_engine(settings)
    worker_id = f"worker-{uuid4().hex[:8]}"
    if embedder is None:
        embedder = build_embedder(settings.embedding)
    if embedder is None:
        logger.error("no embedder configured (set RAG_EMBEDDING__BASE_URL / API key)")
        sys.exit(1)
    while True:
        try:
            with engine.begin() as conn:
                queue.reset_expired_leases(conn, settings.worker.lease_duration_sec)
                job = queue.claim_job(conn, worker_id, settings.worker.lease_duration_sec)
        except OperationalError as exc:
            logger.error("claim failed (is the DB up?): %s", exc)
            if once:
                raise
            time.sleep(settings.worker.poll_interval_sec)
            continue
        if job is None:
            if once:
                return
            time.sleep(settings.worker.poll_interval_sec)
            continue
        try:
            _run_job(engine, job, embedder)
        except Exception as exc:            # noqa: BLE001 — job-level failure handling
            logger.exception("job %s failed", job.id)
            with engine.begin() as conn:
                queue.fail_or_requeue(
                    conn, job.id, job.source_version_id, str(exc),
                    job.attempts, job.max_attempts,
                )
        if once:
            return


def _parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="rag-service ingestion worker")
    parser.add_argument("--once", action="store_true",
                        help="process at most one job, then exit")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    args = _parse_args(argv)
    run_worker(Settings(), once=args.once)


if __name__ == "__main__":
    main()
