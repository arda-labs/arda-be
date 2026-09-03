from fastapi import APIRouter, Depends, Request
from sqlalchemy import Engine

from app.api.deps import security_context
from app.api.sources import get_db
from app.domain.models import FeedbackCreate, FeedbackOut
from app.domain.security import SecurityContext
from app.service import feedback as svc

router = APIRouter()


@router.post("/api/rag/feedback", status_code=201, response_model=FeedbackOut)
def create_feedback(
    data: FeedbackCreate,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.create_feedback(db, ctx, data)
