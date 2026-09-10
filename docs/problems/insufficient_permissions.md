---
code: insufficient_permissions
status: 403
title: Insufficient permissions
summary: |
  The request is authenticated, but the actor does not have a permission for
  the route or cannot manage the target tenant or organization. Tenant
  permissions are evaluated only for the active tenant; global capabilities,
  such as `SUPER_ADMIN`, live in the reserved `system` scope and are evaluated
  separately — they must not be copied into tenant roles.
client_action: |
  - Do not log the user out or retry in a loop.
  - Show an access-denied state and preserve the returned `request_id`.
  - If the user has multiple memberships, allow an explicit tenant switch and
    retry once after the switch.
  - Do not send `X-Tenant-Id`, role, or permission headers from browser code;
    auth-gateway owns those headers.
operator_action: |
  Trace the request by `request_id` and check:
  - The authenticated `user_id` and verified `active_tenant_id`.
  - The route policy permission list in auth-gateway `policy.yaml`.
  - Tenant-scoped roles/permissions and, separately, global roles/permissions.
  - Whether the target `tenant_id` or `organization_id` belongs to the active scope.
  - Whether an old BFF session was refreshed after an IAM authorization change.

  Never diagnose authorization from a browser-supplied identity header.
related_routes:
  - Auth-gateway protected BFF routes
  - Tenant administration routes under /api/admin/tenants
  - Tenant-scoped IAM administration routes under /api/admin/*
  - Organization checks using X-Org-Id
---

The `detail` field carries a human-readable explanation; match on `code`, never
on the message text.
