---
code: upstream_error
status: 502
title: Upstream error
summary: |
  The auth-gateway forwarded the request to the matched upstream service, but
  the HTTP transport failed (`client.Do` returned an error): connection
  refused, DNS/TLS failure, context deadline exceeded, or the client canceling
  the request. No HTTP response from the upstream exists.
client_action: |
  The request may or may not have been processed. For non-idempotent
  operations, check the result before retrying; otherwise retry with backoff.
  If the request was canceled client-side, this code is expected on abort.
operator_action: |
  Correlate `request_id` and `trace_id` across auth-gateway and the named
  upstream, then check:
  - The proxy log `upstream proxy failed` which names the upstream base URL
    and the elapsed `duration_ms` (long durations suggest timeouts).
  - Upstream service health endpoints, readiness probes, and recent restarts
    or OOM kills in the cluster.
  - Network policy / DNS inside the cluster and Cloudflare Tunnel edges.
  - Client disconnects: a canceled request context surfaces here too.

  Responses the upstream did return (including 4xx/5xx) are relayed verbatim
  and never replaced by this code.
related_routes:
  - Auth-gateway BFF proxy routes to IAM, platform, finance, media, workflow
  - CRM, HRM, notification, and MDM service proxy routes
  - AI, RAG, loan, deposit, capital, and statistical service proxy routes
---

See also [proxy_request_invalid](proxy_request_invalid.md) (outbound request
could not be built) and [upstream_not_configured](upstream_not_configured.md)
(no upstream URL for the matched route).
