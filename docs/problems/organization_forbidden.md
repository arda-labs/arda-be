---
code: organization_forbidden
status: 403
title: Organization forbidden
summary: |
  The requested organization is not present in the verified actor's active
  tenant context.
client_action: |
  Clear the stale organization selection and reload organizations; do not
  retry with a different identity header.
operator_action: |
  Compare the organization tenant foreign key with the verified
  `active_tenant_id` and use the request ID to correlate gateway and service
  logs.
related_routes:
  - Organization-scoped routes using X-Org-Id
---

The organization id is tenant-scoped: a valid organization from another tenant
still fails with the same problem.
