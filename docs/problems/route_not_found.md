---
code: route_not_found
status: 404
title: Route not found
summary: |
  The auth-gateway BFF proxy matched no route in `policy.yaml` for the request
  path and HTTP method, so the request was never proxied to any upstream
  service. This is a 404 about the gateway's route table, not about a missing
  business resource.
client_action: |
  - Do not retry unchanged. Check the URL path and HTTP method for typos.
  - If the endpoint should exist, it is either not published through the BFF
    at all or missing from `policy.yaml` — file an issue rather than probing
    paths.
operator_action: |
  Trace `request_id` and check:
  - The gateway proxy log `proxy route denied` with reason `no policy match`.
  - Whether `apps/auth-gateway/configs/policy.yaml` contains a route matching
    the exact path (patterns support `/*` and `/**` suffixes) and method;
    a path match with a non-matching method list also yields this code.
  - Whether a recently added endpoint was deployed to services without
    adding its policy route — clients then hit the gateway first.

  A 404 from the upstream service itself (resource not found) is relayed
  verbatim with the upstream's own problem code instead.
related_routes:
  - All auth-gateway BFF proxy routes
---

Adding an HTTP endpoint requires a matching entry in
`apps/auth-gateway/configs/policy.yaml`; the service itself performs no
authentication or route registration at the gateway boundary.
