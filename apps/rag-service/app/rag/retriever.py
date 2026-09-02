"""P3.1: hybrid retrieval — PostgreSQL FTS + pgvector, fused with RRF.

Tenant/permission filter is spec §5.3 (non-negotiable). Vector leg only runs
when a query_vector is supplied and only over chunks embedded with the
configured model (immutable contract — never mix models). FTS leg uses the
'simple' config (Vietnamese has no built-in config) on content plus
title/heading.
"""

from dataclasses import dataclass

from sqlalchemy import text

from app.config import RetrievalSettings

# Spec §5.3 — the tenant/permission filter, verbatim. s = ai_knowledge_sources,
# v = ai_knowledge_source_versions, c = ai_knowledge_chunks.
_ACCESS_FILTER = """
  AND (s.tenant_id IS NOT DISTINCT FROM :tenant OR s.tenant_id IS NULL)  -- NULL tenant = global/system; accessible to all
  AND s.scope IN ('tenant', 'global')                   -- system never exposed
  AND s.active_version_id = v.id
  AND v.status = 'PUBLISHED'
  AND (s.effective_from IS NULL OR s.effective_from <= now())
  AND (s.effective_to   IS NULL OR s.effective_to   > now())
  AND s.deleted_at IS NULL
"""

_SELECT = """
  SELECT c.chunk_id, c.source_version_id, s.id AS source_id, v.version,
         s.title, c.heading, c.content
"""


@dataclass(frozen=True)
class Candidate:
    chunk_id: str
    source_id: int
    source_version_id: int
    version: str
    title: str
    heading: str
    content: str
    score: float
    source: str   # "vector" | "fts" | "both"


def hybrid_search(
    conn,
    query: str,
    query_vector: list[float] | None,
    *,
    tenant_id: str | None,
    settings: RetrievalSettings,
    model: str = "",
) -> list[Candidate]:
    """Hybrid FTS + vector retrieval, RRF-fused, deduped by chunk_id.

    `conn` is a SQLAlchemy connection; the caller owns the transaction.
    `model` is the embedding model name that chunks must match to be eligible
    for the vector leg (immutable contract — never mix models). Callers pass
    `settings.embedding.model`; tests may pass the configured model directly.
    """
    params = {"tenant": tenant_id, "model": model}

    legs: list[list[tuple[int, str]]] = []   # (rank, chunk_id) per leg
    rows_by_chunk: dict[str, dict] = {}

    # -- vector leg ---------------------------------------------------------
    if query_vector is not None:
        res = conn.execute(
            text(
                _SELECT + """
                  FROM ai_knowledge_chunks c
                  JOIN ai_knowledge_source_versions v ON v.id = c.source_version_id
                  JOIN ai_knowledge_sources s ON s.id = v.source_id
                 WHERE c.embedding IS NOT NULL
                   AND c.embedding_model = :model
                """
                + _ACCESS_FILTER
                + " ORDER BY c.embedding <=> CAST(:vec AS vector) LIMIT :k"
            ),
            {**params, "vec": query_vector, "k": settings.vector_top_k},
        ).mappings().all()
        legs.append([(rank, r["chunk_id"]) for rank, r in enumerate(res, start=1)])
        for r in res:
            rows_by_chunk[r["chunk_id"]] = dict(r)

    # -- FTS leg ------------------------------------------------------------
    res = conn.execute(
        text(
            _SELECT + """
              FROM ai_knowledge_chunks c
              JOIN ai_knowledge_source_versions v ON v.id = c.source_version_id
              JOIN ai_knowledge_sources s ON s.id = v.source_id
             WHERE (to_tsvector('simple', c.content)
                      @@ plainto_tsquery('simple', :q)
                OR to_tsvector('simple', s.title || ' ' || COALESCE(c.heading, ''))
                      @@ plainto_tsquery('simple', :q))
            """
            + _ACCESS_FILTER
            + " LIMIT :k"
        ),
        {**params, "q": query, "k": settings.fts_top_k},
    ).mappings().all()
    legs.append([(rank, r["chunk_id"]) for rank, r in enumerate(res, start=1)])
    for r in res:
        if r["chunk_id"] not in rows_by_chunk:   # vector leg wins the row data
            rows_by_chunk[r["chunk_id"]] = dict(r)

    # -- RRF fusion (score = sum of 1/(k + rank)), dedupe by chunk_id --------
    fused: dict[str, float] = {}
    came_from: dict[str, list[str]] = {}
    for leg_idx, leg in enumerate(legs, start=1):
        leg_name = "vector" if leg_idx == 1 else "fts"
        for rank, chunk_id in leg:
            fused[chunk_id] = fused.get(chunk_id, 0.0) + 1.0 / (settings.rrf_k + rank)
            came_from.setdefault(chunk_id, []).append(leg_name)

    candidates = [
        Candidate(
            chunk_id=cid,
            source_id=row["source_id"],
            source_version_id=row["source_version_id"],
            version=row["version"],
            title=row["title"],
            heading=row["heading"] or "",
            content=row["content"],
            score=score,
            source="both" if len(came_from[cid]) > 1 else came_from[cid][0],
        )
        for cid, score in sorted(fused.items(), key=lambda kv: kv[1], reverse=True)
        if (row := rows_by_chunk.get(cid)) is not None
    ]
    return candidates[: settings.rerank_candidates]
