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