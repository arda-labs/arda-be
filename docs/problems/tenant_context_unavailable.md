---
code: tenant_context_unavailable
status: 403
title: Tenant context unavailable
summary: |
  The actor is authenticated, but IAM did not return a valid active membership
  for a tenant-scoped operation. An empty, `default`, or browser-supplied
  tenant identifier is not a valid fallback.
client_action: |
  Load memberships from `/api/auth/me`, ask the user to select a valid tenant
  when necessary, and retry only after `POST /api/auth/tenant/switch` succeeds.
operator_action: |
  Check IAM membership status, tenant status, and the gateway session refresh
  path.
related_routes:
  - Auth-gateway tenant-scoped BFF routes
  - /api/auth/me
  - /api/auth/tenant/switch
---

Retry without switching tenant will keep failing with the same problem.
