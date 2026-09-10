---
code: ai.auth_required
status: 401
title: Auth required
summary: |
  The request reached ai-service without the auth-gateway's verified-session
  marker (`X-Auth-Checked`), so no usable authenticated session or bearer token
  could be confirmed.
client_action: |
  Send the request through the auth-gateway BFF, starting or refreshing the
  login flow first. Do not set `X-Auth-Checked` or any identity header from
  browser code — the gateway strips and owns those headers.
operator_action: |
  Trace `request_id` and check whether the call bypassed auth-gateway (direct
  service port) or the gateway did not verify and forward the auth marker.
  Confirm the policy route matched in auth-gateway `policy.yaml`.
---

Emitted by the AI agent, conversations, and feedback handlers when
`X-Auth-Checked` is not `true`. ai-service itself performs no login; the
auth-gateway is the single authentication entry point and this check is
defense-in-depth behind it.
