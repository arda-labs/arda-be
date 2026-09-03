from fastapi import APIRouter, Depends, Request
from sqlalchemy import Engine

from app.api.deps import security_context
from app.api.sources import get_db
from app.config import Settings
from app.domain.errors import PermissionDeniedError
from app.domain.models import QueryRequest, QueryResponse
from app.domain.security import SecurityContext
from app.service import query_service as svc

router = APIRouter()


@router.post("/api/rag/query", response_model=QueryResponse)
def query_endpoint(
    data: QueryRequest,
    request: Request,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    # Defense-in-depth: gateway policy rag-query enforces ai.assistant.use;
    # re-check here for services that bypass the gateway (ai-service via
    # service-auth, which does not set X-Permissions).
    if ctx.source_service == "auth-gateway" and "ai.assistant.use" not in ctx.permissions:
        raise PermissionDeniedError("ai.assistant.use required")
    settings: Settings = request.app.state.settings
    return svc.query(db, ctx, data, settings)