# Insufficient permissions

| Field | Value |
| --- | --- |
| `code` | `insufficient_permissions` |
| HTTP status | `403 Forbidden` |
| `type` | `https://docs.arda.io.vn/problems/insufficient_permissions` |
| Content type | `application/problem+json` |

## Meaning

The request is authenticated, but the actor does not have a permission for the
route or cannot manage the target tenant or organization.

Tenant permissions are evaluated only for the active tenant. Global
capabilities, such as `SUPER_ADMIN`, are kept in the reserved `system` scope
and are evaluated separately. They must not be copied into tenant roles.

## Client action

- Do not log the user out or retry in a loop.
- Show an access-denied state and preserve the returned `request_id`.
- If the user has multiple memberships, allow an explicit tenant switch and
  retry once after the switch.
- Do not send `X-Tenant-Id`, role, or permission headers from browser code;
  auth-gateway owns those headers.

## Operator checklist

Trace the request by `request_id` and check:

1. The authenticated `user_id` and verified `active_tenant_id`.
2. The route policy permission list.
3. Tenant-scoped roles/permissions and, separately, global roles/permissions.
4. Whether the target `tenant_id` or `organization_id` belongs to the active
   scope.
5. Whether an old BFF session was refreshed after an IAM authorization change.

Never diagnose authorization from a browser-supplied identity header.

## Example

```json
{
  "type": "https://docs.arda.io.vn/problems/insufficient_permissions",
  "title": "Forbidden",
  "status": 403,
  "code": "insufficient_permissions",
  "detail": "the authenticated actor does not have the required permission",
  "request_id": "req_01J..."
}
```

## Related routes

- Auth-gateway protected BFF routes.
- Tenant administration routes under `/api/admin/tenants`.
- Tenant-scoped IAM administration routes under `/api/admin/*`.
- Organization checks using `X-Org-Id`.
