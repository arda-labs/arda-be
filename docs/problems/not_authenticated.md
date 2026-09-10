---
code: not_authenticated
status: 401
title: Not authenticated
summary: |
  The request reached an auth-gateway BFF route that `policy.yaml` marks
  `auth: true`, but no usable session was found: the session cookie is
  missing, or its session ID is no longer in the auth-gateway session store.
  The gateway also clears the session cookie in this state.
client_action: |
  - Start a fresh login flow and retry once with the new session cookie.
  - Do not retry in a loop and do not forge identity headers such as
    `X-User-Id`; the gateway strips browser-supplied auth-context headers
    before proxying.
operator_action: |
  Trace `request_id` and check:
  - Cookie name, path, domain, and Secure/SameSite attributes versus the
    caller's origin and any proxy in front of the gateway.
  - Session store health (Redis): an expired or evicted session yields this
    code even when the browser still holds the cookie.
  - Whether the route is deliberately protected (`auth: true`) in
    `policy.yaml`.

  The underlying check is `requireAuth && sess == nil` in the gateway proxy
  handler; the denied request is logged with reason `not_authenticated`.
related_routes:
  - Auth-gateway BFF proxy routes marked auth: true in configs/policy.yaml
---

This is the BFF session-cookie path; the generic bearer-token 401 is
[auth.error.unauthorized](auth.error.unauthorized.md). Public routes
(`auth: false`) are proxied without a session and never emit this code.
