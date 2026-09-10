---
code: rag.source_not_found
status: 404
title: Source not found
summary: |
  No knowledge source with that id exists for this tenant, or it was already
  soft-deleted.
client_action: |
  Do not retry unchanged. Verify the source id against
  `GET /api/rag/sources` and refresh stale list views; source ids are
  tenant-scoped.
operator_action: |
  Trace `request_id`, confirm the source row exists in the knowledge store
  and belongs to the caller's tenant, and check whether it was deleted
  concurrently.
---

Emitted by source fetch and delete (`GET`/`DELETE /api/rag/sources/{id}`) and
by version creation, review, and publish when the parent source cannot be
found. Version ids under an existing source return
`rag.version_not_found` instead.
