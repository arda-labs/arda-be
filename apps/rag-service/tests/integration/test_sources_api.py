import hashlib


def _create_source(client, **kw):
    payload = dict(
        title="Test Source",
        description="Integration test source",
        source_type="docs",
        scope="tenant",
        classification="internal",
        language="vi",
        tags=["test", "rag"],
    )
    payload.update(kw)
    r = client.post("/api/rag/sources", json=payload)
    assert r.status_code == 201, r.text
    return r.json()


def test_lifecycle(client):
    # 1. Create source
    src = _create_source(client)
    source_id = src["id"]
    assert src["tenant_id"] == "tenant-a"
    assert src["title"] == "Test Source"
    assert src["source_type"] == "docs"
    assert src["scope"] == "tenant"
    assert src["status"] is None  # no active version yet

    # 2. Create version -> DRAFT
    content = "# Hello\n\nWorld content"
    r = client.post(
        f"/api/rag/sources/{source_id}/versions",
        json={"version": "1.0", "content_type": "markdown", "content": content},
    )
    assert r.status_code == 201, r.text
    ver = r.json()
    version_id = ver["id"]
    assert ver["status"] == "DRAFT"
    assert ver["content_hash"] == hashlib.sha256(content.encode()).hexdigest()
    assert ver["content"] == content

    # 3. Publish before review -> 409
    r = client.post(f"/api/rag/sources/{source_id}/versions/{version_id}/publish")
    assert r.status_code == 409, r.text
    assert r.json()["error"]["code"] == "rag.conflict"

    # 4. Review approve -> APPROVED
    r = client.post(
        f"/api/rag/sources/{source_id}/versions/{version_id}/review",
        json={"decision": "approve", "reason": "Looks good"},
    )
    assert r.status_code == 200, r.text
    ver = r.json()
    assert ver["status"] == "APPROVED"
    assert len(ver["status_history"]) == 1
    assert ver["status_history"][0]["to"] == "APPROVED"

    # 5. Publish -> 202 with job_id
    r = client.post(f"/api/rag/sources/{source_id}/versions/{version_id}/publish")
    assert r.status_code == 202, r.text
    result = r.json()
    assert "job_id" in result
    assert result["version_id"] == version_id
    assert result["status"] == "INDEXING"
    job_id = result["job_id"]

    # 6. Get job -> 200, status in (pending, processing)
    r = client.get(f"/api/rag/jobs/{job_id}")
    assert r.status_code == 200, r.text
    job = r.json()
    assert job["status"] in ("pending", "processing")
    assert job["source_version_id"] == version_id
    assert job["max_attempts"] == 3

    # 7. List excludes deleted
    src2 = _create_source(client, title="To Delete")
    src2_id = src2["id"]
    r = client.delete(f"/api/rag/sources/{src2_id}")
    assert r.status_code == 204
    r = client.get("/api/rag/sources")
    assert r.status_code == 200
    ids = [s["id"] for s in r.json()]
    assert source_id in ids
    assert src2_id not in ids
    # include_deleted brings it back
    r = client.get("/api/rag/sources?include_deleted=true")
    assert r.status_code == 200
    ids = [s["id"] for s in r.json()]
    assert src2_id in ids

    # 8. Duplicate version -> 409
    r = client.post(
        f"/api/rag/sources/{source_id}/versions",
        json={"version": "1.0", "content_type": "markdown", "content": "dup"},
    )
    assert r.status_code == 409, r.text
    assert r.json()["error"]["code"] == "rag.conflict"

    # 9. URL content type -> 501
    r = client.post(
        f"/api/rag/sources/{source_id}/versions",
        json={"version": "2.0", "content_type": "url", "content_url": "https://example.com"},
    )
    assert r.status_code == 501, r.text
    assert r.json()["error"]["code"] == "rag.not_supported_yet"


def test_body_tenant_id_is_rejected(client):
    """Security: tenant identity ONLY from SecurityContext, never body."""
    r = client.post("/api/rag/sources", json={"title": "X", "tenant_id": "other-tenant"})
    assert r.status_code == 201, r.text
    assert r.json()["tenant_id"] == "tenant-a"  # header, not body


def test_body_malformed_effective_from_is_422(client):
    """Malformed ISO date in body → 422, not 500."""
    r = client.post("/api/rag/sources", json={"title": "X", "effective_from": "not-a-date"})
    assert r.status_code == 422, r.text


def test_reject_keeps_draft_and_records_reason(client):
    src = _create_source(client)
    r = client.post(
        f"/api/rag/sources/{src['id']}/versions",
        json={"version": "1.0", "content_type": "markdown", "content": "# x"},
    )
    version_id = r.json()["id"]

    # Reject → stays DRAFT with reason recorded
    r = client.post(
        f"/api/rag/sources/{src['id']}/versions/{version_id}/review",
        json={"decision": "reject", "reason": "not good enough"},
    )
    assert r.status_code == 200, r.text
    ver = r.json()
    assert ver["status"] == "DRAFT"
    assert ver["status_history"][-1]["to"] == "DRAFT"
    assert ver["status_history"][-1]["reason"] == "not good enough"

    # Reviewing an APPROVED version → 409
    r = client.post(
        f"/api/rag/sources/{src['id']}/versions/{version_id}/review",
        json={"decision": "approve"},
    )
    assert r.status_code == 200, r.text
    assert r.json()["status"] == "APPROVED"
    r = client.post(
        f"/api/rag/sources/{src['id']}/versions/{version_id}/review",
        json={"decision": "reject", "reason": "too late"},
    )
    assert r.status_code == 409, r.text
    assert r.json()["error"]["code"] == "rag.conflict"


def test_concurrent_publish_creates_single_job(client):
    """Row lock must serialize concurrent publish: one job, not two."""
    import concurrent.futures
    import os

    from sqlalchemy import create_engine, func, select

    from app.db.schema import ingestion_jobs
    from app.domain.security import SecurityContext
    from app.service.sources import publish_version
    from app.domain.errors import ConflictError

    src = _create_source(client)
    r = client.post(
        f"/api/rag/sources/{src['id']}/versions",
        json={"version": "1.0", "content_type": "markdown", "content": "# x"},
    )
    version_id = r.json()["id"]
    r = client.post(
        f"/api/rag/sources/{src['id']}/versions/{version_id}/review",
        json={"decision": "approve"},
    )
    assert r.status_code == 200

    dsn = os.environ.get("TEST_DATABASE_DSN", "")
    engine = create_engine(dsn, pool_pre_ping=True) if dsn else None
    assert engine is not None, "TEST_DATABASE_DSN must be set"

    ctx = SecurityContext(tenant_id="tenant-a", user_id="user-1",
                          source_service="auth-gateway",
                          permissions=("ai.knowledge.manage",))

    def _publish(_):
        try:
            publish_version(engine, ctx, src["id"], version_id)
            return "ok"
        except ConflictError:
            return "conflict"

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as ex:
        results = list(ex.map(_publish, range(2)))

    assert results.count("ok") == 1, f"exactly one publish must succeed: {results}"
    assert results.count("conflict") == 1, f"one publish must hit 409: {results}"
    with engine.connect() as conn:
        n = conn.execute(
            select(func.count()).select_from(ingestion_jobs).where(
                ingestion_jobs.c.source_version_id == version_id
            )
        ).scalar()
    assert n == 1, f"expected exactly 1 job, got {n}"
    engine.dispose()