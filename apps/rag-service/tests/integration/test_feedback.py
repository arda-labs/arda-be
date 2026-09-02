"""P2.5 integration tests: feedback endpoint.

Requires Postgres (TEST_DATABASE_DSN or the conftest default). Covers:
1. valid run -> 201, fields roundtrip
2. unknown run_id -> 404 with error code rag.not_found
3. missing helpful -> 422
4. malformed run_id -> 422
5. no auth -> 401
"""

import uuid

from sqlalchemy import text


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


def test_feedback_valid_run(client, engine):
    run_id = _seed_run(engine)
    payload = {"run_id": run_id, "helpful": True, "comment": "Great answer"}
    r = client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 201, r.text
    body = r.json()
    assert body["run_id"] == run_id
    assert body["helpful"] is True
    assert body["comment"] == "Great answer"
    assert "id" in body
    assert "created_at" in body


def test_feedback_minimal_body(client, engine):
    """Only run_id and helpful, no comment."""
    run_id = _seed_run(engine)
    payload = {"run_id": run_id, "helpful": False}
    r = client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 201, r.text
    body = r.json()
    assert body["run_id"] == run_id
    assert body["helpful"] is False
    assert body["comment"] is None


def test_feedback_unknown_run_id_is_404(client):
    fake = "00000000-0000-0000-0000-000000000000"
    payload = {"run_id": fake, "helpful": True}
    r = client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 404, r.text
    assert r.json()["error"]["code"] == "rag.not_found"


def test_feedback_missing_helpful_is_422(client):
    payload = {"run_id": str(uuid.uuid4())}
    r = client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 422, r.text


def test_feedback_malformed_run_id_is_422(client):
    payload = {"run_id": "not-a-uuid", "helpful": True}
    r = client.post("/api/rag/feedback", json=payload)
    assert r.status_code == 422, r.text


def test_feedback_no_auth_is_401(client):
    """No x-service-auth header -> 401."""
    from fastapi.testclient import TestClient
    from app.main import create_app
    from app.config import Settings

    settings = Settings(auth_secret="a" * 32, db_dsn="", migrate_on_startup=False)
    app = create_app(settings, migrate_on_startup=False)
    tc = TestClient(app)
    payload = {"run_id": str(uuid.uuid4()), "helpful": True}
    r = tc.post("/api/rag/feedback", json=payload)
    assert r.status_code == 401, r.text