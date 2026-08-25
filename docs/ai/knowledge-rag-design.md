# Knowledge and RAG design

## Purpose

RAG is for approved, versioned knowledge such as procedures, product
documentation, business rules, and operational runbooks. It is not a shadow
copy of every domain table and it must not become a bypass around domain APIs.

## Source lifecycle

```text
register source -> validate owner/scope -> ingest -> chunk -> embed
  -> index -> publish version -> retrieve with ACL filter -> cite
```

Only published, non-expired source versions are retrievable. A source update
creates a new version; it does not silently rewrite the evidence behind an old
answer.

## Scope and authorization

Every source and chunk has an explicit scope:

- `tenant`: visible only to that tenant;
- `global`: approved for all tenants;
- `system`: administrative and never exposed to ordinary assistant prompts.

Retrieval applies scope, classification, publication status, and document-level
ACL filters before vector similarity or full-text ranking is used. Filtering
after retrieval is not sufficient because unauthorized text may already have
reached the model.

## Chunk and embedding policy

- Store source checksum, version, owner, classification, language, and effective
  dates.
- Use deterministic chunk IDs and an ingestion job ID for replay safety.
- Keep chunk text bounded and preserve headings/source location for citations.
- Choose an embedding model and dimension before adding the vector column.
- Keep provider/model/dimension metadata with each embedding set; do not mix
  dimensions in one index.
- Benchmark HNSW/IVFFlat and full-text fallback on representative tenant data
  before selecting an index.

`pgvector` is enabled in the service-owned AI database through an additive
Goose migration. The vector column and index remain disabled until the
provider/dimension and backup/restore, benchmark, and tenant-filtering gates
are complete. Rollback is forward-compatible rather than destructive.

## Retrieval pipeline

1. Normalize the query and resolve active tenant/server identity.
2. Apply authorization filters and query only published source versions.
3. Run hybrid retrieval (metadata/full-text plus vector when enabled).
4. Rerank within the authorized candidate set.
5. Enforce result count, token, and classification limits.
6. Attach stable citations: source ID, version, title, section, and location.
7. Instruct the model to answer only from supplied evidence for knowledge claims.

## Prompt-injection defenses

Retrieved text is data, not instructions. The model prompt must explicitly mark
source content as untrusted evidence. Ignore source requests to reveal secrets,
change policy, call tools, or override system/tenant boundaries. Never let
retrieved text define tool names or permissions.

## Answer quality and fallback

- If evidence is insufficient, say so and ask a focused question or offer a
  permitted live-data tool.
- Include citations for material knowledge claims.
- Record retrieval source IDs and scores in redacted run metadata for evaluation,
  not necessarily in the user transcript.
- Measure groundedness, citation correctness, ACL leakage, stale-source rate,
  retrieval latency, and refusal correctness before expanding scope.
