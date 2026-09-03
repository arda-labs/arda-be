from fastapi.testclient import TestClient

from app.config import Settings
from app.main import create_app


def test_health_live_returns_ok():
    settings = Settings(db_dsn="sqlite:///:memory:")  # placeholder; engine not wired yet
    client = TestClient(create_app(settings, migrate_on_startup=False))
    r = client.get("/health/live")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}


def test_health_ready_returns_ok():
    settings = Settings(db_dsn="sqlite:///:memory:")
    client = TestClient(create_app(settings, migrate_on_startup=False))
    r = client.get("/health/ready")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}
