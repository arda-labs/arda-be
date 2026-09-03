import base64
import hashlib
import hmac
import json
import time

from fastapi import FastAPI, Depends
from fastapi.testclient import TestClient

from app.config import Settings
from app.domain import security
from app.main import create_app
from app.api.deps import security_context

SECRET = "b" * 32


def _token(src: str, aud: str, now: int) -> str:
    claims = {"v": "v1", "src": src, "aud": aud, "iat": now - 5, "exp": now + 120,
              "nonce": base64.urlsafe_b64encode(b"n" * 16).rstrip(b"=").decode()}
    payload = base64.urlsafe_b64encode(json.dumps(claims, separators=(",", ":")).encode()).rstrip(b"=").decode()
    sig = base64.urlsafe_b64encode(
        hmac.new(SECRET.encode(), f"v1.{payload}".encode(), hashlib.sha256).digest()
    ).rstrip(b"=").decode()
    return f"v1.{payload}.{sig}"


def _app() -> FastAPI:
    settings = Settings(auth_secret=SECRET, trusted_sources=["ai-service", "auth-gateway"], db_dsn="")
    application = create_app(settings, migrate_on_startup=False)

    @application.get("/_probe")
    def _probe(ctx: security.SecurityContext = Depends(security_context)) -> dict:
        return {"tenant": ctx.tenant_id, "user": ctx.user_id, "perms": list(ctx.permissions),
                "source": ctx.source_service}

    return application


def test_missing_token_is_401():
    client = TestClient(_app())
    r = client.get("/_probe")
    assert r.status_code == 401


def test_bad_token_is_401():
    client = TestClient(_app())
    r = client.get("/_probe", headers={"x-service-auth": "v1.abc.def"})
    assert r.status_code == 401


def test_unknown_source_is_401_even_with_headers():
    client = TestClient(_app())
    now = int(time.time())
    # token valid, but source not in trusted_sources
    client = TestClient(_app())
    r = client.get("/_probe",
                   headers={"x-service-auth": _token("evil-service", "rag-service", now),
                            "X-Tenant-Id": "tenant-a", "X-User-Id": "user-1"})
    assert r.status_code == 401


def test_trusted_source_gets_context_from_headers():
    client = TestClient(_app())
    now = int(time.time())
    r = client.get("/_probe",
                   headers={"x-service-auth": _token("auth-gateway", "rag-service", now),
                            "X-Tenant-Id": "tenant-a", "X-User-Id": "user-1",
                            "X-Permissions": "ai.knowledge.manage,ai.knowledge.read"})
    assert r.status_code == 200
    body = r.json()
    assert body["tenant"] == "tenant-a" and body["user"] == "user-1"
    assert body["perms"] == ["ai.knowledge.manage", "ai.knowledge.read"]
    assert body["source"] == "auth-gateway"


def test_non_gateway_source_ignores_x_permissions():
    client = TestClient(_app())
    now = int(time.time())
    r = client.get("/_probe",
                   headers={"x-service-auth": _token("ai-service", "rag-service", now),
                            "X-Tenant-Id": "tenant-a", "X-Permissions": "ai.knowledge.manage"})
    assert r.status_code == 200
    body = r.json()
    assert body["tenant"] == "tenant-a"          # tenant still propagates
    assert body["perms"] == []                   # X-Permissions dropped: not gateway
