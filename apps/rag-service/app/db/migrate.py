import logging
import pathlib
from sqlalchemy import Engine, text

logger = logging.getLogger(__name__)
_MIGRATIONS_DIR = pathlib.Path(__file__).resolve().parents[2] / "migrations"
_ADVISORY_LOCK_KEY = 727301001  # rag-service migration lock


def _load_sql_blocks(path: pathlib.Path) -> tuple[str | None, str | None]:
    src = path.read_text(encoding="utf-8")
    up, down = None, None
    in_block = None
    lines = src.splitlines()
    block = []
    for line in lines:
        stripped = line.strip()
        if stripped == "-- +goose Up":
            if up is None:
                in_block, block = "up", []
            continue
        if stripped == "-- +goose Down":
            if in_block == "up":
                up = "\n".join(block)
            in_block, block = "down", []
            continue
        if in_block:
            block.append(line)
    if in_block == "down":
        down = "\n".join(block)
    return up, down


def run_migrations(engine: Engine) -> None:
    """Apply pending migrations under a Postgres advisory lock.

    rag-api runs 2 replicas; the lock guarantees a single writer so two
    instances never apply the same migration concurrently (spec §3.8).
    """
    with engine.begin() as conn:
        conn.execute(text("SELECT pg_advisory_lock(:key)"), {"key": _ADVISORY_LOCK_KEY})
        try:
            conn.execute(text(
                "CREATE TABLE IF NOT EXISTS rag_schema_version ("
                " filename TEXT PRIMARY KEY,"
                " applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"
            ))
            applied = {row[0] for row in conn.execute(text("SELECT filename FROM rag_schema_version"))}
            for path in sorted(_MIGRATIONS_DIR.glob("*.sql")):
                if path.name in applied:
                    continue
                up, _down = _load_sql_blocks(path)
                if not up:
                    raise RuntimeError(f"{path.name}: empty Up block")
                conn.execute(text(up))
                conn.execute(
                    text("INSERT INTO rag_schema_version (filename) VALUES (:f)"),
                    {"f": path.name},
                )
                logger.info("applied migration %s", path.name)
        finally:
            conn.execute(text("SELECT pg_advisory_unlock(:key)"), {"key": _ADVISORY_LOCK_KEY})