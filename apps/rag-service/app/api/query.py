from fastapi import APIRouter, Depends, Request
from sqlalchemy import Engine

from app.api.deps import security_context
from app.api.sources import get_db
from app.config import Settings
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
    settings: Settings = request.app.state.settings
    return svc.query(db, ctx, data, settings)