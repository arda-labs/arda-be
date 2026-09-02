"""P3.3/P3.4 integration tests: POST /api/rag/query.

Seeding pattern (mirrors test_retriever.py): create source + version via the
client API, insert chunks directly with SQL, then flip the version to
PUBLISHED and set active_version_id -- exactly what the worker's atomic
publish does. Self-contained: the seed helpers are copied here, not imported.
"""

import hashlib
import uuid

import pytest
from sqlalchemy import text

from app.config import Settings

CHUNKER_VERSION = "1"
MODEL = "@cf/qwen/qwen3-embedding-0.6b"


def _axis(index: int) -> list[float]:
    """Unit vector along axis `index`, 1024 dimensions. Deterministic."""
    vec = [0.0] * 1024
    vec[index] = 1.0
    return vec


def _sha(s: str) -> str:
    return hashlib.sha256(s.encode("utf-8")).hexdigest()


def _seed_source_version(client, *, title: str, tenant: str | None, scope: str, version: str) -> tuple[int, int]:
    """Create source + markdown version, approve, publish (job left untouched)."""
    hdr = {"X-Tenant-Id": ""} if tenant is None else {"X-Tenant-Id": tenant}
    r = client.post(
        "/api/rag/sources",
        json={"title": title, "description": "query seed", "scope": scope},
        headers=hdr,
    )
    assert r.status_code == 201, r.text
    source_id = r.json()["id"]

    r = client.post(
        f"/api/rag/sources/{source_id}/versions",
        json={
            "version": version,
            "content_type": "markdown",
            "content": f"# {title}\n\nBody of {title}.",
            "chunker_config": {"chunker_version": CHUNKER_VERSION, "chunk_size": 512, "chunk_overlap": 64},
        },
    )
    assert r.status_code == 201, r.text
    version_id = r.json()["id"]

    r = client.post(
        f"/api/rag/sources/{source_id}/versions/{version_id}/review",
        json={"decision": "approve", "reason": "ok"},
    )
    assert r.status_code == 200, r.text
    r = client.post(f"/api/rag/sources/{source_id}/versions/{version_id}/publish")
    assert r.status_code == 202, r.text
    return source_id, version_id


def _seed_chunks(
    engine,
    *,
    source_id: int,
    version_id: int,
    chunks: list[tuple[str, str]],  # (heading, content)
    model: str | None,              # None -> embedding stays NULL
    axis_base: int = 0,
) -> list[str]:
    """Insert chunks directly via SQL, mirroring ingestion.py. Returns chunk_ids."""
    ids = []
    with engine.begin() as conn:
        for i, (heading, content) in enumerate(chunks):
            content_hash = _sha(content)
            chunk_id = hashlib.sha256(
                f"{version_id}:{i}:{content_hash}:{CHUNKER_VERSION}".encode()
            ).hexdigest()
            ids.append(chunk_id)
            if model is None:
                conn.execute(
                    text(
                        "INSERT INTO ai_knowledge_chunks"
                        " (source_version_id, chunk_index, heading, content, chunk_id, content_hash)"
                        " VALUES (:vid, :idx, :heading, :content, :cid, :ch)"
                    ),
                    {"vid": version_id, "idx": i, "heading": heading, "content": content,
                     "cid": chunk_id, "ch": content_hash},
                )
            else:
                conn.execute(
                    text(
                        "INSERT INTO ai_knowledge_chunks"
                        " (source_version_id, chunk_index, heading, content, chunk_id, content_hash,"
                        "  embedding, embedding_model, embedding_dimensions)"
                        " VALUES (:vid, :idx, :heading, :content, :cid, :ch, :emb, :model, 1024)"
                    ),
                    {"vid": version_id, "idx": i, "heading": heading, "content": content,
                     "cid": chunk_id, "ch": content_hash,
                     "emb": _axis(axis_base + i), "model": model},
                )
    # Mirror the worker's atomic publish (complete_job) without running the worker.
    with engine.begin() as conn:
        conn.execute(
            text("UPDATE ai_knowledge_source_versions SET status='PUBLISHED' WHERE id = :vid"),
            {"vid": version_id},
        )
        conn.execute(
            text("UPDATE ai_knowledge_sources SET active_version_id = :vid WHERE id = :sid"),
            {"vid": version_id, "sid": source_id},
        )
    return ids


def _tenant_allowed_source_ids(engine, tenant_id: str | None) -> set[int]:
    """Live source ids visible to `tenant_id`: own + global, never system."""
    with engine.connect() as conn:
        rows = conn.execute(
            text("SELECT id FROM ai_knowledge_sources"
                 " WHERE deleted_at IS NULL AND scope IN ('tenant', 'global')"
                 " AND (tenant_id IS NULL OR tenant_id = :t)"),
            {"t": tenant_id},
        ).mappings().all()
    return {r["id"] for r in rows}


@pytest.fixture()
def qdata(client, engine):
    """tenant-a source (2 chunks), global source, tenant-b source (secret), system source."""
    out = {}
    sid_a, vid_a = _seed_source_version(
        client, title="FAQ nghỉ phép", tenant="tenant-a", scope="tenant", version="1.0")
    out["sid_a"] = sid_a
    out["A0"], out["A1"] = _seed_chunks(
        engine, source_id=sid_a, version_id=vid_a, model=MODEL, axis_base=0,
        chunks=[
            ("Nghỉ phép năm", "Nghỉ phép năm: người lao động được nghỉ phép hưởng nguyên lương. Mỗi năm được nghỉ phép tối đa 12 ngày theo quy định của công ty."),
            ("Quy trình xin nghỉ phép", "Quy trình xin nghỉ phép: gửi đơn qua cổng thông tin nội bộ trước khi nghỉ phép."),
        ],
    )
    sid_b, vid_b = _seed_source_version(
        client, title="Chính sách chung", tenant=None, scope="global", version="1.0")
    out["sid_b"] = sid_b
    out["B0"] = _seed_chunks(
        engine, source_id=sid_b, version_id=vid_b, model=MODEL, axis_base=2,
        chunks=[("Chính sách chung", "Chính sách chung của công ty áp dụng cho tất cả nhân viên.")],
    )[0]
    sid_c, vid_c = _seed_source_version(
        client, title="FAQ nghỉ phép B", tenant="tenant-b", scope="tenant", version="1.0")
    out["sid_c"] = sid_c
    out["C0"] = _seed_chunks(
        engine, source_id=sid_c, version_id=vid_c, model=MODEL, axis_base=3,
        chunks=[("Nghỉ phép tenant B", "Nội dung bí mật của tenant-b về nghỉ phép.")],
    )[0]
    sid_d, vid_d = _seed_source_version(
        client, title="Hệ thống", tenant="tenant-a", scope="system", version="1.0")
    out["sid_d"] = sid_d
    out["D0"] = _seed_chunks(
        engine, source_id=sid_d, version_id=vid_d, model=MODEL, axis_base=4,
        chunks=[("Hệ thống nội bộ", "Hệ thống nội bộ không được truy cập qua tìm kiếm.")],
    )[0]
    return out


def _post(client, json_body):
    return client.post("/api/rag/query", json=json_body)


def _hit_source_ids(body):
    return {h["source_id"] for h in body["hits"]}


# ---------------------------------------------------------------------------
# Tenant isolation
# ---------------------------------------------------------------------------


def test_tenant_isolation(qdata, client, engine):
    """Default fixture is tenant-a: hits are tenant-a + global, never tenant-b/system."""
    r = _post(client, {"query": "nghỉ phép", "top_k": 10})
    assert r.status_code == 200, r.text
    hits = r.json()["hits"]
    assert len(hits) > 0, "must retrieve something for tenant-a"
    allowed = _tenant_allowed_source_ids(engine, "tenant-a")
    assert qdata["sid_a"] in allowed and qdata["sid_b"] in allowed
    assert qdata["sid_c"] not in allowed and qdata["sid_d"] not in allowed
    for h in hits:
        assert h["source_id"] in allowed, f"leak: source {h['source_id']} not visible to tenant-a"


# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------


def test_top_k_bounds(qdata, client):
    assert _post(client, {"query": "nghỉ phép", "top_k": 0}).status_code == 422
    assert _post(client, {"query": "nghỉ phép", "top_k": 11}).status_code == 422
    assert _post(client, {"query": "nghỉ phép", "top_k": 10}).status_code == 200


def test_empty_query_422(qdata, client):
    assert _post(client, {"query": "", "top_k": 3}).status_code == 422
    assert _post(client, {"query": "   ", "top_k": 3}).status_code == 422


# ---------------------------------------------------------------------------
# Response shape + run trace
# ---------------------------------------------------------------------------


def test_response_shape_and_run_row(qdata, client, engine):
    r = _post(client, {"query": "nghỉ phép", "top_k": 3})
    assert r.status_code == 200, r.text
    body = r.json()

    # run_id present + valid uuid
    run_id = body["run_id"]
    uuid.UUID(run_id)

    # counts
    assert body["reranked_count"] == len(body["hits"])
    assert body["retrieved_count"] >= body["reranked_count"]
    assert body["rewritten"] is False, "P3.4: rewritten must always be false in Phase 1"

    # citation format matches seeded heading
    for h in body["hits"]:
        assert h["citation"] == f"[{h['source_id']}:{h['heading']}]", h["citation"]
        assert h["score"] >= 0.0, "score must be a non-negative RRF fused score"

    # ai_rag_runs row exists with traced fields
    with engine.connect() as conn:
        row = conn.execute(
            text("SELECT tenant_id, query, retrieved_count, reranked_count, hit_ids, model_used"
                 " FROM ai_rag_runs WHERE id = :rid"),
            {"rid": run_id},
        ).mappings().one()
    assert row["tenant_id"] == "tenant-a"
    assert row["query"] == "nghỉ phép"
    assert row["retrieved_count"] == body["retrieved_count"]
    assert row["reranked_count"] == body["reranked_count"]
    assert len(row["hit_ids"]) == len(body["hits"]), "hit_ids must match returned hits"
    assert row["model_used"] == MODEL


# ---------------------------------------------------------------------------
# No widening
# ---------------------------------------------------------------------------


def test_body_tenant_ignored(qdata, client, engine):
    """Extra tenant_id in body must be ignored -- no widening to tenant-b."""
    r = _post(client, {"query": "nghỉ phép", "top_k": 10, "tenant_id": "tenant-b"})
    assert r.status_code == 200, r.text
    hits = r.json()["hits"]
    allowed = _tenant_allowed_source_ids(engine, "tenant-a")
    assert len(hits) > 0
    for h in hits:
        assert h["source_id"] in allowed, f"leak via body tenant: source {h['source_id']}"
    assert qdata["sid_c"] not in _hit_source_ids(r.json()), "tenant-b source must not appear"


def test_header_tenant_is_context_no_widening(qdata, client, engine):
    """The X-Tenant-Id header IS the context -- a forged header only changes the
    caller's own scope, never widens it. Fixture default (tenant-a) -> no tenant-b hits."""
    r = _post(client, {"query": "nghỉ phép", "top_k": 10})
    assert r.status_code == 200, r.text
    ids = _hit_source_ids(r.json())
    assert qdata["sid_c"] not in ids, "tenant-b hit must not appear for tenant-a context"


# ---------------------------------------------------------------------------
# Reranker default
# ---------------------------------------------------------------------------


def test_reranker_provider_none_default(qdata, client):
    """reranker.provider defaults to 'none' -- suite needs no ANTHROPIC_API_KEY."""
    settings = Settings()
    assert settings.reranker.provider == "none"
    r = _post(client, {"query": "nghỉ phép", "top_k": 3})
    assert r.status_code == 200, r.text
    assert len(r.json()["hits"]) <= 3


def test_rewritten_always_false_for_ambiguous_query(qdata, client):
    """P3.4: even an ambiguous query is never rewritten in Phase 1."""
    r = _post(client, {"query": "chính sách", "top_k": 3})
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["rewritten"] is False
    # rewritten_query stays NULL in ai_rag_runs too
    with client.app.state.engine.connect() as conn:
        row = conn.execute(
            text("SELECT rewritten_query FROM ai_rag_runs WHERE id = :rid"),
            {"rid": body["run_id"]},
        ).mappings().one()
    assert row["rewritten_query"] is None
