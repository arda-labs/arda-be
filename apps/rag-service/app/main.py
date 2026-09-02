import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api.health import router as health_router
from app.config import Settings

logger = logging.getLogger(__name__)


@asynccontextmanager
async def _lifespan(app: FastAPI):
    settings: Settings = app.state.settings
    if settings.migrate_on_startup:
        try:
            from app.db.engine import get_engine
            from app.db.migrate import run_migrations

            run_migrations(get_engine(settings))
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
    app.include_router(health_router)
    return app


app = create_app(Settings())
