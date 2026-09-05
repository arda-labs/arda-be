# ADR-001: first read-only RAG slice

Status: **ready for implementation; deployment verification pending**
Date: 2026-09-05

## Decision

The first acceptance slice answers questions about the tenant's approved
finance approval and fee policy documents. It is read-only and runs through
`auth-gateway` into `ai-service`; no domain mutation or export is included.

- **Scope:** one tenant acceptance fixture (`tenant-a`) and finance policy
  documents classified `internal`.
- **Retrieval:** published and non-expired versions only; tenant/global ACL is
  evaluated in the database. PostgreSQL FTS is the minimum path; vector search
  and reranking are enabled only when their providers are configured.
- **Evidence contract:** every material policy claim carries `sourceId`,
  `sourceVersionId`, title, version, heading, effective dates, locator and
  deep link when available. If no evidence survives ACL/effective-date filters,
  the answer must say that the system has no supported answer.
- **Protocol:** AG-UI SSE `ag-ui-v1`; exactly one terminal `RUN_FINISHED` or
  `RUN_ERROR`. Cancellation is persisted as `CANCELLED` and retries use a new
  run id.
- **Model:** OpenAI-compatible provider selected by tenant settings. A missing
  or unhealthy provider is an explicit error; no successful placeholder is
  allowed.
- **Quality gate:** `docs/ai/evaluation-set.yaml` is the initial dataset. A
  release requires zero ACL leakage, zero expired-source answers, citation
  presence on known-answer cases, and recorded latency/cost metrics. Numeric
  thresholds are set after the first real corpus run.

## Open release gates

1. Publish approved tenant documents and replace sample source keys in the
   evaluation set.
2. Configure an embedding provider with the migration's 1024-dimension shape,
   then verify ingestion retry/lease recovery and retrieval quality.
3. Run the login → shell → gateway → model SSE → refresh/reopen smoke test in a
   non-production environment.
4. Add answer-level citation validation and an evaluation runner before
   enabling user traffic.

This ADR does not authorize production secrets, database changes, or enabling
HITL/action tools.
