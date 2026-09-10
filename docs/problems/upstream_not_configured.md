---
code: upstream_not_configured
status: 503
title: Upstream not configured
summary: |
  The request matched a `policy.yaml` route, but no upstream base URL is
  configured for its path prefix — the gateway knows the route should be
  proxied yet has nowhere to send it. Nothing was forwarded.
client_action: |
  This is a gateway deployment/configuration fault, not a request problem.
  Retry will not help; report the `request_id` to the operations team.
operator_action: |
  - Check the gateway log `proxy route has no configured upstream`, which
    names the request path.
  - Map the path prefix to its config key in the gateway's upstream table
    (for example `/api/iam` and `/api/admin` to `IAM_SERVICE_URL`, `/api/ai`
    to `AI_SERVICE_URL`, `/api/rag` to `RAG_SERVICE_URL`) and set the missing
    env value, then restart the gateway.
  - A route that exists in `policy.yaml` must never be deployed without its
    upstream URL; keep policy and gateway environment in sync in
    `arda-infra`.
related_routes:
  - All auth-gateway BFF proxy routes
---

The upstream table covers path prefixes `/api/admin`, `/api/iam`,
`/api/platform`, `/api/finance`, `/api/media`, `/api/workflow`, `/api/crm`,
`/api/hrm`, `/api/notifications`, `/api/mdm`, `/api/ai`, `/api/rag`,
`/api/loan`, `/api/deposit`, `/api/capital`, and `/api/statistical`.
