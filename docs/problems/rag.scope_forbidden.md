---
code: rag.scope_forbidden
status: 403
title: Scope forbidden
summary: |
  The knowledge source is not tenant-scoped, and the actor is not a global
  admin: creating, deleting, versioning, reviewing, or publishing `global` or
  `system` sources is reserved for global admins.
client_action: |
  Do not retry unchanged. Ask a global admin to perform the change, or work
  with a tenant-scoped source instead.
operator_action: |
  Trace `request_id` and compare the source's `scope` (`tenant`, `global`, or
  `system`) with the actor's `X-Global-Admin` flag. Only `tenant` scope is
  writable by non-admins.
---

Emitted by the RAG knowledge handlers on source creation with a non-tenant
scope, and on delete, version creation, review, and publish when the existing
source's scope is not `tenant`.
