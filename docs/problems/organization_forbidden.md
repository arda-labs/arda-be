# Organization forbidden

| Field | Value |
| --- | --- |
| `code` | `organization_forbidden` |
| HTTP status | `403 Forbidden` |
| `type` | `https://docs.arda.io.vn/problems/organization_forbidden` |

The requested organization is not present in the verified actor's active
tenant context. The client should clear the stale organization selection and
reload organizations; it must not retry with a different identity header.

Operators should compare the organization tenant foreign key with the verified
`active_tenant_id` and use the request ID to correlate gateway and service
logs.
