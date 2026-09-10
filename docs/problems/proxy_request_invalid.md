---
code: proxy_request_invalid
status: 502
title: Proxy request invalid
summary: |
  The auth-gateway failed to construct the outbound request to the upstream
  service (`http.NewRequestWithContext` returned an error) after policy
  matching and upstream resolution had already passed. No network call was
  attempted.
client_action: |
  Retrying unchanged is unlikely to help; this is a gateway-side failure, not
  a verdict on the request payload. Report the `request_id` to the operations
  team.
operator_action: |
  - This fires only when building the outbound request fails: check the
    request method and the resolved target (upstream base URL plus the
    incoming path and query) for characters that break URL construction, such
    as control characters or malformed percent-encoding.
  - Correlate `request_id` in the auth-gateway proxy log; the response cannot
    name the upstream because no connection was attempted.
  - Distinguish from [upstream_error](upstream_error.md) (the upstream call
    itself failed at transport level) and from upstream 4xx/5xx responses,
    which are relayed verbatim.
related_routes:
  - All auth-gateway BFF proxy routes
---

This is a defensive, rare failure mode; seeing it repeatedly points at
malformed request URLs rather than at upstream health.
