import hashlib
from datetime import datetime, timezone

from sqlalchemy import Engine, select, func, update, insert

from app.domain.errors import (
    ConflictError,
    NotFoundError,
    NotSupportedError,
    PermissionDeniedError,
)
from app.domain.models import (
    JobOut,
    PublishResult,
    ReviewRequest,
    SourceCreate,
    SourceOut,
    VersionCreate,
    VersionOut,
)
from app.domain.security import SecurityContext
from app.db.schema import ingestion_jobs, knowledge_source_versions, knowledge_sources


def _sha256(content: str) -> str:
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


def _check_permission(ctx: SecurityContext) -> None:
    # Mirror of policy.yaml rag-sources-* permissions. ai.admin is reserved
    # for future IAM roles; superadmin is the wildcard sentinel.
    allowed = {"ai.knowledge.manage", "ai.admin", "superadmin", "platform.manage"}
    if ctx.source_service == "auth-gateway" and not allowed.intersection(ctx.permissions):
        raise PermissionDeniedError("ai.knowledge.manage required")


def _effective_tenant(ctx: SecurityContext) -> str | None:
    return ctx.tenant_id


def _now() -> datetime:
    return datetime.now(timezone.utc)


# ---------------------------------------------------------------------------
# Sources
# ---------------------------------------------------------------------------


def create_source(engine: Engine, ctx: SecurityContext, data: SourceCreate) -> SourceOut:
    _check_permission(ctx)
    effective_tenant = _effective_tenant(ctx)
    with engine.begin() as conn:
        row = conn.execute(
            insert(knowledge_sources)
            .values(
                tenant_id=effective_tenant,
                title=data.title,
                description=data.description,
                source_type=data.source_type,
                scope=data.scope,
                classification=data.classification,
                language=data.language,
                tags=data.tags,
                owner_id=data.owner_id,
                effective_from=data.effective_from,
                effective_to=data.effective_to,
                created_by=ctx.user_id,
                updated_at=func.now(),
            )
            .returning(*knowledge_sources.c)
        ).mappings().one()
    return _source_row_to_out(row)


def list_sources(engine: Engine, ctx: SecurityContext, include_deleted: bool = False) -> list[SourceOut]:
    _check_permission(ctx)
    with engine.connect() as conn:
        stmt = select(knowledge_sources).order_by(knowledge_sources.c.created_at.desc())
        if not include_deleted:
            stmt = stmt.where(knowledge_sources.c.deleted_at.is_(None))
        rows = conn.execute(stmt).mappings().all()
        return [_source_row_to_out(r) for r in rows]


def get_source(engine: Engine, ctx: SecurityContext, source_id: int) -> SourceOut:
    _check_permission(ctx)
    with engine.begin() as conn:
        row = conn.execute(
            select(knowledge_sources).where(
                knowledge_sources.c.id == source_id,
                knowledge_sources.c.deleted_at.is_(None),
            )
        ).mappings().one_or_none()
        if row is None:
            raise NotFoundError(f"source {source_id} not found")
        out = _source_row_to_out(row)
        # Fill active version status
        if out.active_version_id:
            vrow = conn.execute(
                select(knowledge_source_versions.c.status, knowledge_source_versions.c.version).where(
                    knowledge_source_versions.c.id == out.active_version_id,
                    knowledge_source_versions.c.source_id == source_id,
                )
            ).mappings().one_or_none()
            if vrow:
                out.status = vrow["status"]
                out.version = vrow["version"]
        return out


def soft_delete_source(engine: Engine, ctx: SecurityContext, source_id: int) -> None:
    _check_permission(ctx)
    with engine.begin() as conn:
        result = conn.execute(
            update(knowledge_sources)
            .where(knowledge_sources.c.id == source_id, knowledge_sources.c.deleted_at.is_(None))
            .values(deleted_at=func.now(), updated_at=func.now())
        )
        if result.rowcount == 0:
            raise NotFoundError(f"source {source_id} not found")


# ---------------------------------------------------------------------------
# Versions
# ---------------------------------------------------------------------------


def create_version(engine: Engine, ctx: SecurityContext, source_id: int, data: VersionCreate) -> VersionOut:
    _check_permission(ctx)
    if data.content_type in ("url", "file"):
        raise NotSupportedError(f"content_type '{data.content_type}' not supported yet")

    content_hash = _sha256(data.content) if data.content else None

    insert_kw = dict(
        source_id=source_id,
        version=data.version,
        content_type=data.content_type,
        content=data.content,
        content_url=data.content_url,
        content_hash=content_hash,
        created_by=ctx.user_id,
        updated_at=func.now(),
    )
    if data.chunker_config:
        insert_kw["chunker_version"] = data.chunker_config.chunker_version
        insert_kw["chunk_size"] = data.chunker_config.chunk_size
        insert_kw["chunk_overlap"] = data.chunker_config.chunk_overlap

    with engine.begin() as conn:
        src = conn.execute(
            select(knowledge_sources.c.id).where(
                knowledge_sources.c.id == source_id, knowledge_sources.c.deleted_at.is_(None)
            )
        ).one_or_none()
        if src is None:
            raise NotFoundError(f"source {source_id} not found")

        try:
            row = conn.execute(
                insert(knowledge_source_versions).values(**insert_kw).returning(*knowledge_source_versions.c)
            ).mappings().one()
        except Exception as e:
            if "unique" in str(e).lower() or "duplicate" in str(e).lower() or "23505" in str(e):
                raise ConflictError(f"version '{data.version}' already exists for source {source_id}") from e
            raise
    return _version_row_to_out(row)


def list_versions(engine: Engine, ctx: SecurityContext, source_id: int) -> list[VersionOut]:
    _check_permission(ctx)
    with engine.connect() as conn:
        rows = conn.execute(
            select(knowledge_source_versions)
            .where(knowledge_source_versions.c.source_id == source_id)
            .order_by(knowledge_source_versions.c.created_at.desc())
        ).mappings().all()
        return [_version_row_to_out(r) for r in rows]


def get_version(engine: Engine, ctx: SecurityContext, source_id: int, version_id: int) -> VersionOut:
    _check_permission(ctx)
    with engine.connect() as conn:
        row = conn.execute(
            select(knowledge_source_versions).where(
                knowledge_source_versions.c.id == version_id,
                knowledge_source_versions.c.source_id == source_id,
            )
        ).mappings().one_or_none()
        if row is None:
            raise NotFoundError(f"version {version_id} not found for source {source_id}")
        return _version_row_to_out(row)


# ---------------------------------------------------------------------------
# State machine
# ---------------------------------------------------------------------------


def review_version(
    engine: Engine, ctx: SecurityContext, source_id: int, version_id: int, data: ReviewRequest
) -> VersionOut:
    _check_permission(ctx)
    if data.decision not in ("approve", "reject"):
        raise ConflictError("decision must be 'approve' or 'reject'")

    now_iso = _now().isoformat()
    with engine.begin() as conn:
        row = conn.execute(
            select(knowledge_source_versions)
            .where(
                knowledge_source_versions.c.id == version_id,
                knowledge_source_versions.c.source_id == source_id,
            )
            .with_for_update()
        ).mappings().one_or_none()
        if row is None:
            raise NotFoundError(f"version {version_id} not found for source {source_id}")

        if row["status"] != "DRAFT":
            raise ConflictError(f"cannot review version in status '{row['status']}'")

        new_status = "APPROVED" if data.decision == "approve" else "DRAFT"
        status_entry = {"to": new_status, "at": now_iso, "by": ctx.user_id, "reason": data.reason}
        current_history = list(row["status_history"] or [])
        current_history.append(status_entry)

        updated = conn.execute(
            update(knowledge_source_versions)
            .where(knowledge_source_versions.c.id == version_id)
            .values(status=new_status, status_history=current_history, updated_at=func.now())
            .returning(*knowledge_source_versions.c)
        ).mappings().one()
    return _version_row_to_out(updated)


def publish_version(
    engine: Engine, ctx: SecurityContext, source_id: int, version_id: int
) -> PublishResult:
    _check_permission(ctx)
    with engine.begin() as conn:
        row = conn.execute(
            select(knowledge_source_versions)
            .where(
                knowledge_source_versions.c.id == version_id,
                knowledge_source_versions.c.source_id == source_id,
            )
            .with_for_update()
        ).mappings().one_or_none()
        if row is None:
            raise NotFoundError(f"version {version_id} not found for source {source_id}")

        current_status = row["status"]
        if current_status != "APPROVED":
            raise ConflictError(f"cannot publish version in status '{current_status}'; must be APPROVED")

        # Create ingestion job
        job_row = conn.execute(
            insert(ingestion_jobs)
            .values(source_version_id=version_id, max_attempts=3)
            .returning(*ingestion_jobs.c)
        ).mappings().one()

        # Append status_history entry
        now_iso = _now().isoformat()
        status_entry = {"to": "INDEXING", "at": now_iso, "by": ctx.user_id, "reason": "publish"}
        current_history = list(row["status_history"] or [])
        current_history.append(status_entry)

        conn.execute(
            update(knowledge_source_versions)
            .where(knowledge_source_versions.c.id == version_id)
            .values(status="INDEXING", status_history=current_history, updated_at=func.now())
        )

    return PublishResult(job_id=str(job_row["id"]), version_id=version_id, status="INDEXING")


# ---------------------------------------------------------------------------
# Jobs
# ---------------------------------------------------------------------------


def get_job(engine: Engine, ctx: SecurityContext, job_id: str) -> JobOut:
    _check_permission(ctx)
    with engine.connect() as conn:
        row = conn.execute(
            select(ingestion_jobs).where(ingestion_jobs.c.id == job_id)
        ).mappings().one_or_none()
        if row is None:
            raise NotFoundError(f"job {job_id} not found")
        return _job_row_to_out(row)


# ---------------------------------------------------------------------------
# Row mappers
# ---------------------------------------------------------------------------


def _source_row_to_out(row) -> SourceOut:
    tags = list(row["tags"]) if row.get("tags") else []
    return SourceOut(
        id=row["id"],
        tenant_id=row.get("tenant_id"),
        title=row["title"],
        description=row.get("description"),
        source_type=row["source_type"],
        scope=row["scope"],
        classification=row["classification"],
        language=row.get("language"),
        tags=tags,
        owner_id=row.get("owner_id"),
        effective_from=row.get("effective_from"),
        effective_to=row.get("effective_to"),
        active_version_id=row.get("active_version_id"),
        deleted_at=row.get("deleted_at"),
        created_by=row.get("created_by"),
        created_at=row.get("created_at"),
        updated_at=row.get("updated_at"),
    )


def _version_row_to_out(row) -> VersionOut:
    return VersionOut(
        id=row["id"],
        source_id=row["source_id"],
        version=row["version"],
        status=row["status"],
        content_type=row["content_type"],
        content=row.get("content"),
        content_url=row.get("content_url"),
        chunker_version=row.get("chunker_version"),
        chunk_size=row.get("chunk_size"),
        chunk_overlap=row.get("chunk_overlap"),
        content_hash=row.get("content_hash"),
        status_history=list(row["status_history"] or []) if row.get("status_history") else [],
        created_by=row.get("created_by"),
        created_at=row.get("created_at"),
        updated_at=row.get("updated_at"),
    )


def _job_row_to_out(row) -> JobOut:
    return JobOut(
        id=str(row["id"]),
        source_version_id=row["source_version_id"],
        status=row["status"],
        locked_by=row.get("locked_by"),
        locked_at=row.get("locked_at"),
        attempts=row.get("attempts") or 0,
        max_attempts=row.get("max_attempts") or 3,
        error_message=row.get("error_message"),
        total_chunks=row.get("total_chunks") or 0,
        embedded_chunks=row.get("embedded_chunks") or 0,
        next_retry_at=row.get("next_retry_at"),
        created_at=row.get("created_at"),
        updated_at=row.get("updated_at"),
    )