import os

from sqlalchemy import text

from app.config import Settings
from app.db.engine import get_engine
from app.db.migrate import run_migrations

DSN = os.environ.get("TEST_DATABASE_DSN", "postgresql+psycopg://postgres:postgres@localhost:5432/postgres")


def _clean(engine) -> None:
    with engine.begin() as conn:
        conn.execute(text("DROP TABLE IF EXISTS ai_rag_eval, ai_rag_feedback, ai_rag_runs, ai_ingestion_jobs, ai_knowledge_chunks, ai_knowledge_source_versions, ai_knowledge_sources CASCADE"))
        conn.execute(text("DROP TABLE IF EXISTS rag_schema_version"))


def test_migrations_apply_and_are_idempotent():
    engine = get_engine(Settings(db_dsn=DSN, migrate_on_startup=False))
    _clean(engine)
    try:
        run_migrations(engine)
        run_migrations(engine)  # second run must be a no-op
        with engine.connect() as conn:
            tables = {row[0] for row in conn.execute(text("SELECT tablename FROM pg_tables WHERE schemaname='public'"))}
            assert {"ai_knowledge_sources", "ai_knowledge_source_versions", "ai_knowledge_chunks", "ai_ingestion_jobs", "ai_rag_runs", "ai_rag_feedback", "ai_rag_eval"} <= tables
            ledger = {r[0] for r in conn.execute(text("SELECT filename FROM rag_schema_version"))}
            assert ledger == {"20260903090000_rag_foundation.sql", "20260903090001_rag_eval.sql",
                              "20260903090002_rag_add_version_content.sql"}
    finally:
        _clean(engine)
        engine.dispose()
