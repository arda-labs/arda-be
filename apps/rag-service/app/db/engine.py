from sqlalchemy import Engine, create_engine

from app.config import Settings


def get_engine(settings: Settings) -> Engine:
    dsn = settings.db_dsn
    if dsn.startswith(("postgres://", "postgresql://")):
        dsn = "postgresql+psycopg://" + dsn.split("://", 1)[1]
    return create_engine(dsn, pool_pre_ping=True)