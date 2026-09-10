---
code: auth.error.unauthorized
status: 401
title: Authentication required
summary: |
  The request has no valid BFF session or bearer token, so the server cannot
  identify the caller.
client_action: |
  Start or refresh the login flow, then repeat the request with a fresh
  session. Do not treat this as an authorization failure and do not attempt to
  repair it by adding identity headers.
operator_action: |
  Trace `request_id`, inspect cookie/CORS/origin handling, and confirm that
  auth-gateway can resolve the IAM user context.
related_routes:
  - Auth-gateway protected BFF routes
  - All /api/* routes behind the session cookie or bearer token
---

Credentials must never be written to logs or problem responses.
