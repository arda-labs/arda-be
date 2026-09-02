import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from app.api.feedback import router as feedback_router
from app.api.health import router as health_router
from app.api.sources import router as sources_router
from app.config import Settings
from app.domain.errors import RagError

logger = logging.getLogger(__name__)


@asynccontextmanager
async def _lifespan(app: FastAPI):
    if app.state.do_migrate:
        try:
            from app.db.engine import get_engine
            from app.db.migrate import run_migrations

            run_migrations(get_engine(app.state.settings))
        except ImportError:
            logger.warning("migrations skipped: app.db.migrate not available yet")
    yield


def create_app(settings: Settings, migrate_on_startup: bool | None = None) -> FastAPI:
    app = FastAPI(
        title="rag-service",
        version="0.1.0",
        docs_url=None,
        redoc_url=None,
        lifespan=_lifespan,
    )
    app.state.settings = settings
    app.state.do_migrate = settings.migrate_on_startup if migrate_on_startup is None else migrate_on_startup

    @app.exception_handler(RagError)
    def _rag_error_handler(_request: Request, exc: RagError) -> JSONResponse:
        return JSONResponse(
            status_code=exc.status_code,
            content={"error": {"code": exc.code, "message": str(exc)}},
        )

    app.include_router(health_router)
    app.include_router(sources_router)
    app.include_router(feedback_router)
    return app


app = create_app(Settings())
