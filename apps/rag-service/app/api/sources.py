from fastapi import APIRouter, Depends, File, Form, Request, UploadFile
from sqlalchemy import Engine

from app.api.deps import security_context
from app.domain.errors import RagError
from app.domain.models import (
    ChunkPreviewRequest,
    ChunkPreviewResponse,
    JobOut,
    PublishResult,
    ReviewRequest,
    SourceCreate,
    SourceOut,
    VersionCreate,
    VersionOut,
)
from app.domain.security import SecurityContext
from app.service import sources as svc

router = APIRouter()



def get_db(request: Request) -> Engine:
    engine = getattr(request.app.state, "engine", None)
    if engine is None:
        from app.db.engine import get_engine
        engine = get_engine(request.app.state.settings)
        request.app.state.engine = engine
    return engine


# ---------------------------------------------------------------------------
# Sources
# ---------------------------------------------------------------------------


@router.post("/api/rag/sources", status_code=201, response_model=SourceOut)
def create_source(
    data: SourceCreate,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.create_source(db, ctx, data)


@router.get("/api/rag/sources", response_model=list[SourceOut])
def list_sources(
    include_deleted: bool = False,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.list_sources(db, ctx, include_deleted=include_deleted)


@router.get("/api/rag/sources/{source_id}", response_model=SourceOut)
def get_source(
    source_id: int,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.get_source(db, ctx, source_id)


@router.delete("/api/rag/sources/{source_id}", status_code=204)
def delete_source(
    source_id: int,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    svc.soft_delete_source(db, ctx, source_id)


# ---------------------------------------------------------------------------
# Versions
# ---------------------------------------------------------------------------


@router.post("/api/rag/sources/{source_id}/versions", status_code=201, response_model=VersionOut)
def create_version(
    source_id: int,
    data: VersionCreate,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.create_version(db, ctx, source_id, data)


@router.get("/api/rag/sources/{source_id}/versions", response_model=list[VersionOut])
def list_versions(
    source_id: int,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.list_versions(db, ctx, source_id)


@router.get("/api/rag/sources/{source_id}/versions/{version_id}", response_model=VersionOut)
def get_version(
    source_id: int,
    version_id: int,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.get_version(db, ctx, source_id, version_id)


# ---------------------------------------------------------------------------
# Review / Publish
# ---------------------------------------------------------------------------


@router.post("/api/rag/sources/{source_id}/versions/{version_id}/review", response_model=VersionOut)
def review_version(
    source_id: int,
    version_id: int,
    data: ReviewRequest,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.review_version(db, ctx, source_id, version_id, data)


@router.post("/api/rag/sources/{source_id}/versions/{version_id}/publish", status_code=202, response_model=PublishResult)
def publish_version(
    source_id: int,
    version_id: int,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.publish_version(db, ctx, source_id, version_id)


# ---------------------------------------------------------------------------
# Jobs
# ---------------------------------------------------------------------------


@router.get("/api/rag/jobs/{job_id}", response_model=JobOut)
def get_job(
    job_id: str,
    ctx: SecurityContext = Depends(security_context),
    db: Engine = Depends(get_db),
):
    return svc.get_job(db, ctx, job_id)


# ---------------------------------------------------------------------------
# Preview & Ingestion testing
# ---------------------------------------------------------------------------


@router.post("/api/rag/sources/preview-chunks", response_model=ChunkPreviewResponse)
def preview_chunks(
    data: ChunkPreviewRequest,
    ctx: SecurityContext = Depends(security_context),
):
    return svc.preview_chunks(ctx, data)


@router.post("/api/rag/sources/parse-preview", response_model=ChunkPreviewResponse)
async def parse_and_preview(
    file: UploadFile = File(...),
    chunk_size: int = Form(512),
    chunk_overlap: int = Form(64),
    ctx: SecurityContext = Depends(security_context),
):
    contents = await file.read()
    return svc.parse_and_preview_file(
        ctx,
        file_bytes=contents,
        filename=file.filename or "document.txt",
        chunk_size=chunk_size,
        chunk_overlap=chunk_overlap,
    )
