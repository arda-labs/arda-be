# Authentication required

| Field | Value |
| --- | --- |
| `code` | `auth.error.unauthorized` |
| HTTP status | `401 Unauthorized` |
| `type` | `https://docs.arda.io.vn/problems/auth.error.unauthorized` |

The request has no valid BFF session or bearer token. The client may start the
login flow. It should not treat this as an authorization failure or attempt to
repair it by adding identity headers.

Operators should trace `request_id`, inspect cookie/CORS/origin handling, and
confirm that auth-gateway can resolve the IAM user context. Credentials must
never be written to logs or problem responses.
