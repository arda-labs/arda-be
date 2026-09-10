---
code: auth.error.forbidden
status: 403
title: Permission denied
summary: |
  The actor is authenticated but the generic auth policy does not allow the
  requested operation. For route-specific explanations, clients may receive
  [insufficient_permissions](insufficient_permissions.md).
client_action: |
  Do not log the user out. Preserve the request ID, show access denied, and let
  the user know they lack access to this feature.
operator_action: |
  Compare the matched route policy in auth-gateway `policy.yaml` with the
  verified tenant and global capabilities of the actor.
related_routes:
  - Auth-gateway protected BFF routes
  - policy.yaml-gated routes
---

Error messages may be localized or improved without changing the `code` or
`type`.
