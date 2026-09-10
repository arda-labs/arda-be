---
code: ai_runtime_identity_unavailable
status: 502
title: AI runtime identity unavailable
summary: |
  For requests to `/api/ai/*` or `/api/rag/*`, the auth-gateway must mint a
  short-lived (60 second) service identity token signing `auth-gateway` as
  source and `ai-service` as audience, using the shared workload secret
  (`ARDA_SERVICE_AUTH_SECRET`). Minting failed, so the request was never
  forwarded to ai-service.
client_action: |
  This is a gateway configuration fault, not a request problem. Retry will
  not help; report the `request_id` to the operations team.
operator_action: |
  - Check that `ARDA_SERVICE_AUTH_SECRET` is set on the auth-gateway
    deployment and at least 32 characters; `identity.Issue` rejects shorter
    or empty secrets, which is the only way this code is emitted.
  - Verify the secret is synchronized across gateway and services in
    `arda-infra`; a mismatch surfaces at ai-service as
    `ai.service_auth_required` (401), not here.
  - ai-service verifies this token in its `ServiceAuthMiddleware` on every
    non-health route, so failures here break all `/api/ai/*` and
    `/api/rag/*` traffic at once.
related_routes:
  - Auth-gateway BFF proxy routes under /api/ai/*
  - Auth-gateway BFF proxy routes under /api/rag/*
---

The minted token is sent in the service identity metadata header; downstream
user/tenant context still travels in the `X-User-*` / `X-Tenant-Id` headers
set after session verification.
