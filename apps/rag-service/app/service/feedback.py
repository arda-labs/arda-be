from sqlalchemy import Engine, insert
from sqlalchemy.exc import IntegrityError

from app.db.schema import rag_feedback
from app.domain.errors import NotFoundError
from app.domain.models import FeedbackCreate, FeedbackOut
from app.domain.security import SecurityContext
from app.service.sources import _check_permission


def create_feedback(engine: Engine, ctx: SecurityContext, data: FeedbackCreate) -> FeedbackOut:
    _check_permission(ctx)
    with engine.begin() as conn:
        try:
            row = conn.execute(
                insert(rag_feedback)
                .values(run_id=data.run_id, helpful=data.helpful, comment=data.comment)
                .returning(*rag_feedback.c)
            ).mappings().one()
        except IntegrityError as e:
            # psycopg code 23503 = foreign_key_violation
            if "23503" in str(e) or getattr(getattr(e, "orig", None), "sqlstate", "") == "23503":
                raise NotFoundError(f"run {data.run_id} not found") from e
            raise
    return FeedbackOut(
        id=str(row["id"]),
        run_id=str(row["run_id"]),
        helpful=row["helpful"],
        comment=row.get("comment"),
        created_at=row.get("created_at"),
    )