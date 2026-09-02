import base64
import hashlib
import hmac
import json
import time

import pytest

from app.domain import security


def _go_style_token(secret: str, src: str, aud: str, iat: int, exp: int) -> str:
    claims = {
        "v": "v1",
        "src": src,
        "aud": aud,
        "iat": iat,
        "exp": exp,
        "nonce": base64.urlsafe_b64encode(b"0123456789abcdef").rstrip(b"=").decode(),
    }
    payload = base64.urlsafe_b64encode(json.dumps(claims, separators=(",", ":")).encode()).rstrip(b"=").decode()
    signing = hmac.new(secret.encode(), f"v1.{payload}".encode(), hashlib.sha256).digest()
    sig = base64.urlsafe_b64encode(signing).rstrip(b"=").decode()
    return f"v1.{payload}.{sig}"


SECRET = "a" * 32  # >= 32 chars per Go contract
NOW = int(time.time())


def test_verify_go_compatible_token():
    token = _go_style_token(SECRET, "ai-service", "rag-service", NOW - 10, NOW + 120)
    claims = security.verify_service_token(token, SECRET, "rag-service")
    assert claims.source == "ai-service"
    assert claims.audience == "rag-service"


def test_verify_rejects_wrong_audience():
    token = _go_style_token(SECRET, "ai-service", "other-service", NOW - 10, NOW + 120)
    with pytest.raises(security.AuthenticationError):
        security.verify_service_token(token, SECRET, "rag-service")


def test_verify_rejects_expired():
    token = _go_style_token(SECRET, "ai-service", "rag-service", NOW - 300, NOW - 60)
    with pytest.raises(security.AuthenticationError):
        security.verify_service_token(token, SECRET, "rag-service")


def test_verify_rejects_tampered_signature():
    token = _go_style_token(SECRET, "ai-service", "rag-service", NOW - 10, NOW + 120)
    parts = token.split(".")
    parts[1] = parts[1][:-2] + ("A" if parts[1][-1] != "A" else "B")
    with pytest.raises(security.AuthenticationError):
        security.verify_service_token(".".join(parts), SECRET, "rag-service")
