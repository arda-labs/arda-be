# User context unavailable

| Field | Value |
| --- | --- |
| `code` | `user_context_unavailable` |
| HTTP status | `401 Unauthorized` |
| `type` | `https://docs.arda.io.vn/problems/user_context_unavailable` |

The session is present but the gateway could not refresh a current IAM user
context. The client should clear the local session and start login again; it
should not retry the protected request in a loop.

Operators should correlate the request ID across auth-gateway and IAM, then
check IAM readiness, identity lookup, tenant membership loading, and role or
permission queries. Do not expose the underlying database or service error to
the browser.
