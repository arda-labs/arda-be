from sqlalchemy import Engine, create_engine

from app.config import Settings


def get_engine(settings: Settings) -> Engine:
    return create_engine(settings.db_dsn, pool_pre_ping=True)