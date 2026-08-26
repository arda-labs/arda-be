# Knowledge Ingestion Pipeline

Status: **Design specification — implement with Knowledge RAG Gate (after Gate 7)**.
Covers how knowledge content enters the system, goes through review, and becomes
retrievable by the AI assistant.

---

## 1. Overview

The ingestion pipeline is the pathway from raw content to a retrievable,
ACL-filtered knowledge chunk. It is separate from the retrieval pipeline
(covered in `knowledge-rag-design.md`) and from the embedding/indexing process.

```
Content Author / Admin
  │ upload file or register URL
  ▼
Source Registration (scope, classification, owner, effective dates)
  │
  ▼
Content Validation (format, encoding, size, content policy check)
  │
  ▼
Chunking (deterministic, idempotent, preserves heading/location)
  │
  ▼
Review Gate (manual or auto-approved, depends on classification)
  │ approved
  ▼
Embedding (when vector column is enabled)
  │
  ▼
Index + Publish Version
  │
  ▼
Available for Retrieval (ACL-filtered, scope-aware)
```

---

## 2. Who Can Publish Knowledge

| Actor | Scope | Approval Required |
|:---|:---|:---|
| Platform admin | `global` | Yes — second admin review |
| Tenant admin | `tenant` | Configurable per tenant (default: auto-approved) |
| System process | `system` | Never exposed to assistant prompts |

No end user may directly publish knowledge. Content is always submitted through
an admin-owned interface or a CI pipeline.

---

## 3. Source Types

| Type | Description | Example |
|:---|:---|:---|
| `document` | PDF, DOCX, Markdown file | Operational runbook, product manual |
| `url` | Static HTML page fetched periodically | Public API documentation |
| `snippet` | Short plain-text fragment entered directly | Business rule, FAQ answer |
| `structured` | JSON/CSV transformed into text chunks | Lookup table, pricing matrix |

---

## 4. Source Registration API

```http
POST /api/ai/knowledge/sources
Authorization: Bearer <admin-token>
Content-Type: application/json

{
  "title": "CRM Customer Onboarding Runbook",
  "sourceType": "document",
  "scope": "tenant",          // "tenant" | "global" | "system"
  "classification": "internal",
  "language": "vi",
  "effectiveFrom": "2026-09-01",
  "effectiveTo": null,        // null = no expiry
  "tags": ["crm", "onboarding", "runbook"]
}
```

Response includes `sourceId` and an upload URL (pre-signed S3/MinIO URL) for
document types, or a `snippetContent` field for snippet types.

---

## 5. Chunking Policy

Chunking is **deterministic and idempotent** — re-ingesting the same content
with the same settings produces the same chunk IDs.

### Chunk ID derivation

```
chunkId = sha256(sourceId + "|" + chunkIndex + "|" + contentChecksum)
```

This means re-ingestion detects unchanged chunks (same checksum → skip embedding
re-computation) and identifies changed chunks (different checksum → re-embed).

### Chunk size targets

| Source type | Target chunk size | Max size |
|:---|:---:|:---:|
| Document (prose) | 512 tokens | 768 tokens |
| Document (table/list) | 256 tokens | 512 tokens |
| URL | 512 tokens | 768 tokens |
| Snippet | Entire content | 512 tokens |

Chunks preserve:
- Heading hierarchy (H1/H2/H3 as metadata, included in retrieval context)
- Source page/section reference for citations
- Contiguous sentences — no mid-sentence splits

### Overlap

A 64-token overlap between adjacent chunks preserves cross-boundary context.
Overlap chunks share their `chunkIndex` prefix with their parent chunk.

---

## 6. Review Gate

### Auto-approval policy (default)

| Scope | Classification | Default |
|:---|:---|:---|
| `tenant` | `internal` | Auto-approved |
| `tenant` | `confidential` | Manual review required |
| `global` | Any | Manual review by second admin |
| `system` | Any | CI pipeline only, no human gate |

### Review states

```
PENDING_REVIEW → APPROVED → PUBLISHED
             ↓
           REJECTED (with reason)
```

An approved source is not yet retrievable. It moves to `PUBLISHED` after
embedding and indexing complete. This prevents partially-indexed content from
being retrievable.

### Review API

```http
POST /api/ai/knowledge/sources/{id}/review
Authorization: Bearer <reviewer-token>

{
  "decision": "approve",   // or "reject"
  "reason": ""             // required for reject
}
```

Self-review is rejected (reviewer must not be the source owner).

---

## 7. Embedding & Indexing

Embedding runs as a background Goose-managed job after approval.

**Current state:** Embedding is disabled until the embedding model, dimension,
and vector index are selected (per `knowledge-rag-design.md` gates). Approved
content is chunked and stored but not embedded; full-text search is available
immediately.

**When embedding is enabled:**

1. The ingestion job reads each approved, unembedded chunk.
2. Calls the configured embedding provider (same credential management as the
   chat model provider; see `multi-provider-design.md`).
3. Stores the embedding vector alongside `embedding_model_id` and
   `embedding_dimension` in `ai_knowledge_chunks`.
4. Updates chunk status to `EMBEDDED`.
5. Triggers HNSW index rebuild (or incremental insert depending on pgvector
   version).

**Idempotency:** The job records a `job_id` per batch. Re-running after failure
skips already-embedded chunks (same `chunkId`).

---

## 8. Source Versioning & Updates

A knowledge update creates a **new version**, not a mutation of existing chunks:

```
source v1 (PUBLISHED)
  → update submitted
  → source v2 (PENDING_REVIEW)
  → approved + embedded
  → source v2 (PUBLISHED)
  → source v1 (RETIRED)  ← retrieval now uses v2 only
```

**v1 chunks are retained** in `ai_knowledge_chunks` for audit and citation
resolution of old conversations, but are excluded from new retrieval queries
via `source.status = 'PUBLISHED'` filter.

Rollback: setting v2 to `RETIRED` and v1 back to `PUBLISHED` restores the
previous version. This is an admin-only operation.

---

## 9. Expiry & Retention

Sources with `effectiveTo` in the past are automatically moved to `EXPIRED`
by a scheduled job (daily). Expired sources are excluded from retrieval.

Source deletion:
- Soft-delete only: sets `status = 'DELETED'`.
- Chunks are soft-deleted with the source.
- Hard deletion (including vector data) runs after the source retention policy
  window (default 90 days post-deletion).
- Tenant offboarding triggers immediate soft-delete of all tenant-scoped sources
  and schedules hard deletion per data-governance policy.

---

## 10. Observability

| Event | Recorded in |
|:---|:---|
| Source registered | `audit_observability` event |
| Review decision | `audit_observability` event |
| Chunking started/completed | Structured log + metric |
| Embedding started/completed/failed | Structured log + metric |
| Source published/retired/expired | `audit_observability` event |
| Ingestion job failure | Alert on repeated failures |

Metrics:
- Ingestion pipeline lag (time from approval to PUBLISHED)
- Embedding success/failure rate
- Chunk count per source version
- Embedding latency per chunk
