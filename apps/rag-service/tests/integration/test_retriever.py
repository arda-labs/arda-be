"""P3.1 integration tests: hybrid FTS + pgvector retriever with RRF.

Seeding pattern (established in test_queue_worker.py): create source + version
via the client API, then insert chunks directly with SQL. The worker is NOT
run; instead the version is manually flipped to PUBLISHED and the source's
active_version_id is set, mirroring exactly what the worker's atomic publish
(complete_job) does.
"""

import hashlib

import pytest
from sqlalchemy import text

from app.config import Settings
from app.rag.retriever import hybrid_search

CHUNKER_VERSION = "1"
MODEL = "@cf/qwen/qwen3-embedding-0.6b"
OTHER_MODEL = "some-other-model"


def _axis(index: int) -> list[float]:
    """Unit vector along axis `index`, 1024 dimensions. Deterministic."""
    vec = [0.0] * 1024
    vec[index] = 1.0
    return vec


def _sha(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def _seed_source_version(client, *, title: str, tenant: str | None, scope: str, version: str) -> tuple[int, int]:
    """Create source (scoped tenant + scope) + markdown version; review approve; publish (job ignored).

    Returns (source_id, version_id). The publish endpoint creates an ingestion
    job which is left untouched -- the worker is not run in these tests.
    """
    # Override the fixture's default X-Tenant-Id when tenant is not the default.
    # An empty-string value un-sets the header (the gateway reads it as None).
    hdr = {"X-Tenant-Id": ""} if tenant is None else {"X-Tenant-Id": tenant}
    r = client.post(
        "/api/rag/sources",
        json={"title": title, "description": "retriever seed", "scope": scope},
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
    axis_base: int = 0,             # chunk i gets unit vector along axis (axis_base + i)
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


@pytest.fixture()
def std_ids(client, engine):
    """Sources A..E from the brief. Returns {label: chunk_id}."""
    out = {}
    sid_a, vid_a = _seed_source_version(
        client, title="FAQ nghỉ phép", tenant="tenant-a", scope="tenant", version="1.0")
    out["A0"], out["A1"] = _seed_chunks(
        engine, source_id=sid_a, version_id=vid_a, model=MODEL, axis_base=0,
        chunks=[
            ("Nghỉ phép năm", "Nghỉ phép năm: người lao động được nghỉ phép hưởng nguyên lương. Mỗi năm được nghỉ phép tối đa 12 ngày theo quy định của công ty."),
            ("Quy trình xin nghỉ phép", "Quy trình xin nghỉ phép: gửi đơn qua cổng thông tin nội bộ trước khi nghỉ phép."),
        ],
    )
    sid_b, vid_b = _seed_source_version(
        client, title="Chính sách chung", tenant=None, scope="global", version="1.0")
    out["B0"] = _seed_chunks(
        engine, source_id=sid_b, version_id=vid_b, model=MODEL, axis_base=2,
        chunks=[("Chính sách chung", "Chính sách chung của công ty áp dụng cho tất cả nhân viên.")],
    )[0]
    sid_c, vid_c = _seed_source_version(
        client, title="FAQ nghỉ phép B", tenant="tenant-b", scope="tenant", version="1.0")
    out["C0"] = _seed_chunks(
        engine, source_id=sid_c, version_id=vid_c, model=MODEL, axis_base=3,
        chunks=[("Nghỉ phép tenant B", "Nội dung bí mật của tenant-b về nghỉ phép.")],
    )[0]
    sid_d, vid_d = _seed_source_version(
        client, title="Hệ thống", tenant="tenant-a", scope="system", version="1.0")
    out["D0"] = _seed_chunks(
        engine, source_id=sid_d, version_id=vid_d, model=MODEL, axis_base=4,
        chunks=[("Hệ thống nội bộ", "Hệ thống nội bộ không được truy cập qua tìm kiếm.")],
    )[0]
    sid_e, vid_e = _seed_source_version(
        client, title="FAQ nghỉ phép chưa nhúng", tenant="tenant-a", scope="tenant", version="1.0")
    out["E0"] = _seed_chunks(
        engine, source_id=sid_e, version_id=vid_e, model=None,
        chunks=[("Nghỉ phép không nhúng", "Chính sách nghỉ phép chưa được nhúng vector.")],
    )[0]
    return out


def _search(conn, query: str, qv: list[float] | None, *, tenant_id: str | None):
    settings = Settings(retrieval={"vector_top_k": 8, "fts_top_k": 8, "rrf_k": 60,
                                   "rerank_candidates": 8, "final_top_k": 3})
    return hybrid_search(conn, query, qv, tenant_id=tenant_id, settings=settings.retrieval,
                         model=MODEL)


def _query_zero():
    """Unit vector along axis 0: closest to A0, orthogonal to every other chunk."""
    return _axis(0)


def test_hybrid_returns_tenant_and_global_never_other_tenants_or_system(std_ids, engine):
    conn = engine.connect()
    try:
        hits = _search(conn, "nghỉ phép", _query_zero(), tenant_id="tenant-a")
        got = {h.chunk_id for h in hits}
        assert std_ids["A0"] in got, "tenant-a chunk must be retrievable"
        assert std_ids["B0"] in got, "global chunk must be retrievable by tenant-a"
        assert std_ids["C0"] not in got, "tenant-b chunk must never leak"
        assert std_ids["D0"] not in got, "system chunk must never be exposed"
        for h in hits:
            assert h.chunk_id in {std_ids["A0"], std_ids["A1"], std_ids["B0"], std_ids["E0"]}
    finally:
        conn.close()


def test_vector_leg_requires_matching_embedding_model(std_ids, client, engine):
    # Chunk with a different embedding_model seeded via the same pattern
    sid, vid = _seed_source_version(
        client, title="FAQ nghỉ phép khác", tenant="tenant-a", scope="tenant", version="1.0")
    other_id = _seed_chunks(
        engine, source_id=sid, version_id=vid, model=OTHER_MODEL,
        chunks=[("Nghỉ phép khác", "Nội dung khác về nghỉ phép với model khác.")],
    )[0]

    conn = engine.connect()
    try:
        # FTS path: the chunk may appear because its content matches
        hits = _search(conn, "nghỉ phép", _query_zero(), tenant_id="tenant-a")
        got = {h.chunk_id for h in hits}
        assert other_id in got, "chunk with different model may still appear via FTS"

        # Vector-only path (no FTS terms): different-model chunk must NOT appear
        vec_only = _search(conn, "zzz", _query_zero(), tenant_id="tenant-a")
        assert other_id not in {h.chunk_id for h in vec_only}
    finally:
        conn.close()


def test_unembedded_chunk_via_fts_only(std_ids, engine):
    conn = engine.connect()
    try:
        hits = _search(conn, "nghỉ phép", _query_zero(), tenant_id="tenant-a")
        assert std_ids["E0"] in {h.chunk_id for h in hits}

        # Vector-only path (no FTS lexical match): unembedded chunk is invisible
        vec_only = _search(conn, "zzz", _query_zero(), tenant_id="tenant-a")
        assert std_ids["E0"] not in {h.chunk_id for h in vec_only}
    finally:
        conn.close()


def test_rrf_ranks_both_legs_first_and_dedupes(std_ids, engine):
    conn = engine.connect()
    try:
        hits = _search(conn, "nghỉ phép", _query_zero(), tenant_id="tenant-a")
        got = {h.chunk_id for h in hits}
        assert std_ids["A0"] in got
        assert std_ids["B0"] in got
        assert len(got) == len(hits), "no duplicate chunk_id in output"
        assert len(hits) <= 8, "output must be capped at rerank_candidates"

        # A0 matches both legs -> its fused score must beat any single-leg chunk
        score_a0 = next(h.score for h in hits if h.chunk_id == std_ids["A0"])
        single_leg = [h for h in hits if h.chunk_id in (std_ids["A1"], std_ids["B0"])]
        for h in single_leg:
            assert score_a0 > h.score, (
                f"chunk in both legs ({score_a0}) must rank above single-leg ({h.score})"
            )

        # A0 came from both legs
        a0 = next(h for h in hits if h.chunk_id == std_ids["A0"])
        assert a0.source == "both"
    finally:
        conn.close()


def test_fts_leg_order_is_deterministic_by_relevance(std_ids, engine):
    """FTS-only path (query_vector=None): ORDER BY GREATEST(ts_rank(...)) must be deterministic.

    A0's content mentions 'nghỉ phép' most frequently (3x) -> ranks first.
    The exact order of lower-ranked chunks is ts_rank-implementation-dependent
    (density vs frequency), but A0 must always be first.
    """
    conn = engine.connect()
    try:
        hits = _search(conn, "nghỉ phép", None, tenant_id="tenant-a")
        ranked = [h.chunk_id for h in hits]
        assert ranked[0] == std_ids["A0"], f"A0 must rank first in the FTS leg, got {ranked}"
    finally:
        conn.close()


def test_null_tenant_returns_only_global_and_null_tenant(std_ids, engine):
    conn = engine.connect()
    try:
        hits = _search(conn, "nghỉ phép", _query_zero(), tenant_id=None)
        got = {h.chunk_id for h in hits}
        assert std_ids["B0"] in got, "NULL-tenant global source must be retrievable"
        assert std_ids["A0"] not in got, "tenant-a source must not match NULL tenant query"
        assert std_ids["C0"] not in got
        assert std_ids["D0"] not in got
        for h in hits:
            assert h.chunk_id == std_ids["B0"]
    finally:
        conn.close()