# Forbidden

| Field | Value |
| --- | --- |
| `code` | `auth.error.forbidden` |
| HTTP status | `403 Forbidden` |
| `type` | `https://docs.arda.io.vn/problems/auth.error.forbidden` |

The actor is authenticated but the generic auth policy does not allow the
requested operation. For route-specific explanations, clients may receive
[`insufficient_permissions`](insufficient_permissions.md).

Do not log the user out. Preserve the request ID, show access denied, and ask
an operator to compare the matched route policy with the verified tenant and
global capabilities.
