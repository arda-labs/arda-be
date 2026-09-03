"""P2.5 integration tests: feedback endpoint.

Requires Postgres (TEST_DATABASE_DSN or the conftest default). Covers:
1. valid run -> 201, fields roundtrip
2. unknown run_id -> 404 with error code rag.not_found
3. missing helpful -> 422
4. malformed run_id -> 422
5. no auth -> 401
6. ai-service source (no X-Permissions) -> 201
7. gateway without ai.assistant.use -> 403
"""

import base64
import hashlib
import hmac
import json
import time
import uuid

from fastapi.testclient import TestClient
from sqlalchemy import text

from app.config import Settings
from app.main import create_app
from tests.conftest import SECRET


def _seed_run(engine) -> str:
    """Insert a row into ai_rag_runs and return its id."""
    run_id = str(uuid.uuid4())
    with engine.begin() as conn:
        conn.execute(
            text(
                "INSERT INTO ai_rag_runs (id, query) VALUES (:id, :query)"
            ),
            {"id": run_id, "query": "test query"},
        )
    return run_id


def _service_token(secret: str = SECRET, source: str = "ai-service") -> str:
    """Build a v1.{claims}.{hmac} service token (source signs, no permissions)."""
    now = int(time.time())
    claims = {
        "v": "v1", "src": source, "aud": "rag-service",
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


def _service_client(engine, source: str = "ai-service", headers: dict | None = None) -> TestClient:
    """A client carrying a non-gateway service token (X-Permissions dropped)."""
    settings = Settings(
        auth_secret=SECRET,
        trusted_sources=["ai-service", "auth-gateway"],
        db_dsn="",
        migrate_on_startup=False,
    )
    app = create_app(settings, migrate_on_startup=False)
    app.state.engine = engine
    default = {"x-service-auth": _service_token(source=source), "X-Tenant-Id": "tenant-a"}
    return TestClient(app, headers=headers or default)


def test_feedback_valid_run(assistant_client, engine):
    run_id = _seed_run(engine)
    payload = {"run_id": run_id, "helpful": True, "comment": "Great answer"}
    r = assistant_client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 201, r.text
    body = r.json()
    assert body["run_id"] == run_id
    assert body["helpful"] is True
    assert body["comment"] == "Great answer"
    assert "id" in body
    assert "created_at" in body


def test_feedback_minimal_body(assistant_client, engine):
    """Only run_id and helpful, no comment."""
    run_id = _seed_run(engine)
    payload = {"run_id": run_id, "helpful": False}
    r = assistant_client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 201, r.text
    body = r.json()
    assert body["run_id"] == run_id
    assert body["helpful"] is False
    assert body["comment"] is None


def test_feedback_unknown_run_id_is_404(assistant_client):
    fake = "00000000-0000-0000-0000-000000000000"
    payload = {"run_id": fake, "helpful": True}
    r = assistant_client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 404, r.text
    assert r.json()["error"]["code"] == "rag.not_found"


def test_feedback_missing_helpful_is_422(assistant_client):
    payload = {"run_id": str(uuid.uuid4())}
    r = assistant_client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 422, r.text


def test_feedback_malformed_run_id_is_422(assistant_client):
    payload = {"run_id": "not-a-uuid", "helpful": True}
    r = assistant_client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 422, r.text


def test_feedback_ai_service_source_allowed(engine):
    """ai-service token (no X-Permissions) is trusted; permissions tuple is empty."""
    run_id = _seed_run(engine)
    payload = {"run_id": run_id, "helpful": True}
    r = _service_client(engine).post("/api/rag/feedback", json=payload)
    assert r.status_code == 201, r.text
    assert r.json()["run_id"] == run_id


def test_feedback_gateway_without_assistant_use_is_403(engine):
    """Gateway token whose X-Permissions lacks ai.assistant.use -> 403."""
    run_id = _seed_run(engine)
    payload = {"run_id": run_id, "helpful": True}
    r = _service_client(
        engine,
        source="auth-gateway",
        headers={
            "x-service-auth": _service_token(source="auth-gateway"),
            "X-Tenant-Id": "tenant-a",
            "X-User-Id": "user-1",
            "X-Permissions": "ai.knowledge.manage",
        },
    ).post("/api/rag/feedback", json=payload)
    assert r.status_code == 403, r.text
    assert r.json()["error"]["code"] == "rag.forbidden"


def test_feedback_no_auth_is_401():
    """No x-service-auth header -> 401."""
    settings = Settings(auth_secret=SECRET, db_dsn="", migrate_on_startup=False)
    app = create_app(settings, migrate_on_startup=False)
    tc = TestClient(app)
    payload = {"run_id": str(uuid.uuid4()), "helpful": True}
    r = tc.post("/api/rag/feedback", json=payload)
    assert r.status_code == 401, r.text