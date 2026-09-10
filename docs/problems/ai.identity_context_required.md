---
code: ai.identity_context_required
status: 401
title: Identity context required
summary: |
  Auth was verified, but the request carries no usable `X-User-Id` or
  `X-Tenant-Id`, so the handler cannot scope the action to an actor and tenant.
client_action: |
  Retry through the auth-gateway with a fresh session. Do not add
  `X-User-Id`/`X-Tenant-Id` headers from client code — the gateway injects
  them from the verified session and strips client-supplied values.
operator_action: |
  Trace `request_id` and check why auth-gateway accepted the request but did
  not propagate the identity headers on the matched `policy.yaml` route.
---

Emitted by the same handlers as `ai.auth_required`, but only after
`X-Auth-Checked` was `true`: the gateway verified the session yet sent an
empty user or tenant header.
