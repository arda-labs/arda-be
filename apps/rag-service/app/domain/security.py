import base64
import datetime as dt
import hashlib
import hmac
import json
from dataclasses import dataclass

_MAX_CLOCK_SKEW = dt.timedelta(seconds=30)


class AuthenticationError(Exception):
    """x-service-auth missing, malformed, or failed verification."""


@dataclass(frozen=True)
class VerifiedClaims:
    source: str
    audience: str


@dataclass(frozen=True)
class SecurityContext:
    tenant_id: str | None = None
    user_id: str | None = None
    permissions: tuple[str, ...] = ()
    source_service: str = ""
    auth_checked: bool = False


def _sign(secret: str, value: str) -> str:
    digest = hmac.new(secret.encode(), value.encode(), hashlib.sha256).digest()
    return base64.urlsafe_b64encode(digest).rstrip(b"=").decode()


def verify_service_token(
    token: str,
    secret: str,
    expected_audience: str,
    now: dt.datetime | None = None,
) -> VerifiedClaims:
    secret = secret.strip()
    if len(secret) < 32:
        raise AuthenticationError("secret too short")
    parts = token.split(".")
    if len(parts) != 3 or parts[0] != "v1" or not parts[1] or not parts[2]:
        raise AuthenticationError("malformed token")
    expected = _sign(secret, f"v1.{parts[1]}")
    if not hmac.compare_digest(expected, parts[2]):
        raise AuthenticationError("bad signature")
    try:
        claims = json.loads(base64.urlsafe_b64decode(parts[1] + "=" * (-len(parts[1]) % 4)))
    except Exception:
        raise AuthenticationError("bad claims") from None
    if claims.get("v") != "v1" or not claims.get("src") or not claims.get("aud") or not claims.get("nonce"):
        raise AuthenticationError("bad claims")
    if claims["aud"] != expected_audience.strip():
        raise AuthenticationError("wrong audience")
    now = (now or dt.datetime.now(dt.timezone.utc)).timestamp()
    try:
        iat, exp = int(claims.get("iat") or 0), int(claims.get("exp") or 0)
    except (TypeError, ValueError):
        raise AuthenticationError("invalid lifetime") from None
    if iat <= 0 or exp <= iat or now < iat - _MAX_CLOCK_SKEW.total_seconds() or not (now < exp):
        raise AuthenticationError("invalid lifetime")
    return VerifiedClaims(source=claims["src"], audience=claims["aud"])
