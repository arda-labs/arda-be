import base64
import hashlib
import hmac
import json
import os
import time

import pytest
from fastapi.testclient import TestClient
from sqlalchemy import create_engine, text

from app.config import Settings
from app.db.migrate import run_migrations
from app.main import create_app

TEST_DSN = os.environ.get(
    "TEST_DATABASE_DSN",
    "postgresql+psycopg://arda_super:123456@192.168.10.201:30432/rag_test",
)
SECRET = "a" * 32


def _clean(engine):
    with engine.begin() as conn:
        for t in [
            "ai_rag_feedback", "ai_rag_runs",
            "ai_ingestion_jobs", "ai_knowledge_chunks",
            "ai_knowledge_source_versions", "ai_knowledge_sources",
        ]:
            conn.execute(text(f"DROP TABLE IF EXISTS {t} CASCADE"))
        conn.execute(text("DROP TABLE IF EXISTS rag_schema_version"))


def _clean_data(engine):
    with engine.begin() as conn:
        conn.execute(
            text(
                "TRUNCATE ai_ingestion_jobs, ai_knowledge_source_versions, "
                "ai_knowledge_sources RESTART IDENTITY CASCADE"
            )
        )


def _gateway_token(secret: str = SECRET, aud: str = "rag-service") -> str:
    now = int(time.time())
    claims = {
        "v": "v1", "src": "auth-gateway", "aud": aud,
        "iat": now - 5, "exp": now + 120,
        "nonce": base64.urlsafe_b64encode(b"n" * 16).rstrip(b"=").decode(),
    }
    payload = base64.urlsafe_b64encode(
        json.dumps(claims, separators=(",", ":")).encode()
    ).rstrip(b"=").decode()
    sig = base64.urlsafe_b64encode(
        hmac.new(secret.encode(), f"v1.{payload}".encode(), hashlib.sha256).digest()
    ).rstrip(b"=").decode()
    return f"v1.{payload}.{sig}"


@pytest.fixture(scope="session")
def engine():
    eng = create_engine(TEST_DSN, pool_pre_ping=True)
    _clean(eng)
    run_migrations(eng)
    yield eng
    eng.dispose()


@pytest.fixture()
def client(engine):
    _clean_data(engine)
    settings = Settings(
        auth_secret=SECRET,
        trusted_sources=["ai-service", "auth-gateway"],
        db_dsn=TEST_DSN,
        migrate_on_startup=False,
    )
    app = create_app(settings, migrate_on_startup=False)
    app.state.engine = engine
    token = _gateway_token()
    with TestClient(
        app,
        headers={
            "x-service-auth": token,
            "X-Tenant-Id": "tenant-a",
            "X-User-Id": "user-1",
            "X-Permissions": "ai.knowledge.manage",
        },
    ) as c:
        yield c


@pytest.fixture()
def assistant_client(engine):
    """Gateway-authenticated client with the chat-permission set (ai.assistant.use)."""
    _clean_data(engine)
    settings = Settings(
        auth_secret=SECRET,
        trusted_sources=["ai-service", "auth-gateway"],
        db_dsn=TEST_DSN,
        migrate_on_startup=False,
    )
    app = create_app(settings, migrate_on_startup=False)
    app.state.engine = engine
    token = _gateway_token()
    with TestClient(
        app,
        headers={
            "x-service-auth": token,
            "X-Tenant-Id": "tenant-a",
            "X-User-Id": "user-1",
            "X-Permissions": "ai.assistant.use",
        },
    ) as c:
        yield c
