---
code: recent_auth_required
status: 403
title: Recent auth required
summary: |
  The session is valid, but the matched route is marked `risk: high` in
  auth-gateway `policy.yaml` and the session's last strong authentication
  (`AuthTime`, set at login and refreshed by step-up) is older than the
  re-authentication window — `recent_auth_window_seconds`, 300 seconds by
  default. The gateway requires step-up authentication before proxying
  high-risk requests.
client_action: |
  - Do not log the user out; the session itself is fine. Retrying unchanged
    keeps failing until step-up succeeds.
  - Complete `POST /api/auth/step-up` with an MFA `code` (enrolled users) or
    `{"confirm": true}` (users without MFA), then retry the original request
    once.
  - Preflight sensitive actions with `GET /api/auth/recent-auth`, which
    returns `recentAuthOk`, `stepUpUntil`, and `validSeconds`.
operator_action: |
  Trace `request_id` and check:
  - The route matched in `policy.yaml` and its `risk: high` marking — only
    high-risk routes trigger this check; a zero window disables it entirely.
  - `recent_auth_window_seconds` (env `RECENT_AUTH_WINDOW_SECONDS`) versus the
    session's `AuthTime` in the session store.
  - The proxy log line `proxy route denied` with reason
    `recent_auth_required`.

  A mistyped OTP returns `auth.step_up.invalid_code`, never the global 401
  session-loss code, so step-up failures must not clear the browser session.
related_routes:
  - POST /api/auth/step-up
  - GET /api/auth/recent-auth
  - Auth-gateway routes marked risk: high in configs/policy.yaml
---

Emitted as the problem `type` URI
`https://docs.arda.io.vn/problems/recent_auth_required` (status 403, body
`application/problem+json`). Match on `code`, never on the message text. For
a missing session on the same route see [not_authenticated](not_authenticated.md).
