from fastapi import Request, HTTPException

from app.config import Settings
from app.domain import security


def security_context(request: Request) -> security.SecurityContext:
    settings: Settings = request.app.state.settings
    secret = settings.auth_secret
    if not secret:
        raise HTTPException(status_code=401, detail="rag.auth_not_configured")
    token = request.headers.get("x-service-auth", "")
    try:
        claims = security.verify_service_token(token, secret, settings.service_name)
    except security.AuthenticationError:
        raise HTTPException(status_code=401, detail="rag.service_auth_required") from None
    if claims.source not in settings.trusted_sources:
        raise HTTPException(status_code=401, detail="rag.service_auth_required")
    headers = request.headers
    perms: tuple[str, ...] = ()
    if claims.source == "auth-gateway":            # only gateway has checked policy
        perms = tuple(p.strip() for p in headers.get("X-Permissions", "").split(",") if p.strip())
    return security.SecurityContext(
        tenant_id=headers.get("X-Tenant-Id") or None,
        user_id=headers.get("X-User-Id") or None,
        permissions=perms,
        source_service=claims.source,
        auth_checked=bool(headers.get("X-Auth-Checked")),
    )
