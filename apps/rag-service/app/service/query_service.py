"""P3.3: query pipeline — security filter -> RRF -> rerank -> citations, run traced.

Pipeline order (spec sec.6.1 invariant, non-negotiable):
  SecurityContext -> SQL-filtered retrieval (vector + fts) -> RRF -> rerank(top_k)
  -> clamp to min(top_k, settings.retrieval.final_top_k) -> citation projection.

The service is synchronous (per ruling 5) -- FastAPI runs it in the threadpool.
"""

import logging
import time

from sqlalchemy import Engine, insert

from app.config import Settings
from app.domain.models import QueryHitOut, QueryRequest, QueryResponse
from app.domain.security import SecurityContext
from app.db.schema import rag_runs
from app.rag.embedder import EmbeddingError, build_embedder
from app.rag.reranker import build_reranker
from app.rag.retriever import hybrid_search

logger = logging.getLogger(__name__)


def query(engine: Engine, ctx: SecurityContext, data: QueryRequest, settings: Settings) -> QueryResponse:
    t0 = time.perf_counter()
    query_text = data.query.strip()
    top_k = data.top_k
    model = settings.embedding.model

    # -- Embedding (optional, never fails the query) ---------------------------
    embedder = build_embedder(settings.embedding)
    query_vector = None
    if embedder is not None:
        try:
            query_vector = embedder.embed([query_text])[0]
        except EmbeddingError:
            logger.warning("embedding dimension mismatch, falling back to FTS-only", exc_info=True)
        except Exception:
            logger.warning("embedding error, falling back to FTS-only", exc_info=True)

    # -- Retrieval + RRF (hybrid_search does both) -----------------------------
    with engine.connect() as conn:
        candidates = hybrid_search(
            conn, query_text, query_vector,
            tenant_id=ctx.tenant_id,
            settings=settings.retrieval,
            model=model,
        )
    retrieved_count = len(candidates)

    # -- Rerank ----------------------------------------------------------------
    reranker = build_reranker(settings.reranker)
    if reranker is not None:
        candidates = reranker.rerank(query_text, candidates, top_k)

    # -- Clamp to final_top_k --------------------------------------------------
    final_top = min(top_k, settings.retrieval.final_top_k)
    hits = candidates[:final_top]

    # -- Citation projection ---------------------------------------------------
    out_hits = [
        QueryHitOut(
            source_id=h.source_id,
            source_version_id=h.source_version_id,
            version=h.version,
            title=h.title,
            heading=h.heading,
            content=h.content,
            score=h.score,
            citation=f"[{h.source_id}:{h.heading}]",
        )
        for h in hits
    ]

    # -- Persist run -----------------------------------------------------------
    reranked_count = len(out_hits)
    latency_ms = int((time.perf_counter() - t0) * 1000)
    hit_ids = [h.chunk_id for h in hits]

    with engine.begin() as conn:
        row = conn.execute(
            insert(rag_runs)
            .values(
                tenant_id=ctx.tenant_id,
                query=query_text,
                rewritten_query=None,
                retrieved_count=retrieved_count,
                reranked_count=reranked_count,
                hit_ids=hit_ids,
                latency_ms=latency_ms,
                model_used=model,
            )
            .returning(rag_runs.c.id)
        ).mappings().one()

    return QueryResponse(
        run_id=str(row["id"]),
        hits=out_hits,
        latency_ms=latency_ms,
        rewritten=False,                      # P3.4 -- always false in Phase 1
        retrieved_count=retrieved_count,
        reranked_count=reranked_count,
    )