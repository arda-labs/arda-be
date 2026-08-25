# Tenant context unavailable

| Field | Value |
| --- | --- |
| `code` | `tenant_context_unavailable` |
| HTTP status | `403 Forbidden` |
| `type` | `https://docs.arda.io.vn/problems/tenant_context_unavailable` |

The actor is authenticated, but IAM did not return a valid active membership
for a tenant-scoped operation. The client should load memberships from
`/api/auth/me`, ask the user to select a valid tenant when necessary, and retry
only after `POST /api/auth/tenant/switch` succeeds.

An empty, `default`, or browser-supplied tenant identifier is not a valid
fallback. Operators should check IAM membership status, tenant status, and the
gateway session refresh path.
