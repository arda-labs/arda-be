---
code: user_context_unavailable
status: 401
title: User context unavailable
summary: |
  The session is present but the gateway could not refresh a current IAM user
  context.
client_action: |
  Clear the local session and start login again; do not retry the protected
  request in a loop.
operator_action: |
  Correlate the request ID across auth-gateway and IAM, then check IAM
  readiness, identity lookup, tenant membership loading, and role or
  permission queries. Do not expose the underlying database or service error to
  the browser.
related_routes:
  - Auth-gateway proxy routes that require a refreshed IAM user context
---

Never diagnose authorization from a browser-supplied identity header.
