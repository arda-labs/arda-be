---
code: policy_unavailable
status: 503
title: Policy unavailable
summary: |
  The auth-gateway BFF proxy has no route policy loaded (`policy == nil`), so
  the gateway cannot evaluate any request against `policy.yaml` and fails
  closed: nothing is proxied, including otherwise-public routes.
client_action: |
  This is a gateway configuration fault, not a request problem. Retry a few
  times with backoff, then report the `request_id` to the operations team.
operator_action: |
  The gateway normally exits at startup when `policy.Load` fails, so a running
  gateway should never hold a nil policy. If this fires:
  - Check auth-gateway startup logs for `load policy` errors and verify the
    policy file path in the deployment configuration.
  - Verify `apps/auth-gateway/configs/policy.yaml` parses and that the config
    mounted into the pod matches the committed file.
  - Expect every proxied request (any path, any method) to return this code
    until the gateway restarts with a loaded policy.
related_routes:
  - All auth-gateway BFF proxy routes
---

Distinguish this from [route_not_found](route_not_found.md), where the policy
loaded fine but the request simply matched no route.
