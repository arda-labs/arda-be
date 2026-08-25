# Tenant Context Contract

## Ownership

- Kratos owns credentials and authentication.
- IAM owns Arda users, tenants, memberships, roles, permissions and tenant authorization.
- auth-gateway owns the browser session and active tenant context.
- Domain services consume the verified context and never resolve tenants from browser input, email addresses or legacy defaults.

## Canonical data model

```text
iam_users
  id = Arda principal

iam_tenants
  id, code, name, status

iam_tenant_memberships
  user_id, tenant_id, status, is_default

iam_organizations
  tenant_id, code, ...
```

`iam_users.tenant_id` remains a compatibility field during migration. New
authorization decisions use `iam_tenant_memberships` and the active tenant
context. `system` is a reserved control-plane scope for superadmin and is not
an entry in the business tenant registry. `default` is not a valid runtime
tenant.

## Login and switching

```text
Kratos authenticates identity
  -> auth-gateway resolves IAM user context
  -> IAM returns active membership and tenant-scoped roles
  -> gateway stores activeTenantId in the BFF session
  -> gateway injects X-Tenant-Id into downstream requests
```

The browser can request a tenant switch through:

```text
POST /api/auth/tenant/switch
{ "tenant_id": "..." }
```

The gateway asks IAM to validate an active membership before updating the
session. The browser cannot set `X-Tenant-Id` directly.

## Response context

`GET /api/auth/me` returns the existing user context plus:

```json
{
  "activeTenantId": "tenant-id",
  "tenantMemberships": [
    {
      "tenantId": "tenant-id",
      "tenantCode": "arda-internal",
      "tenantName": "Arda Internal",
      "tenantStatus": "ACTIVE",
      "status": "ACTIVE",
      "isDefault": true
    }
  ],
  "tenantSelectionRequired": false
}
```

Roles, permissions, groups and organizations in the active context are scoped
to `activeTenantId`. The context also exposes control-plane capabilities
separately:

```json
{
  "roles": ["TENANT_ADMIN"],
  "permissions": ["iam.user.read"],
  "globalRoles": ["SUPER_ADMIN"],
  "globalPermissions": ["superadmin"],
  "isGlobalAdmin": true,
  "globalCapabilitiesLoaded": true
}
```

Global capabilities are read from the reserved `system` scope and are never
merged into tenant `roles` or `permissions`. Only the trusted auth-gateway may
forward `X-Global-Roles`, `X-Global-Permissions`, and `X-Global-Admin` to an
internal service. Domain services should continue to authorize ordinary data
access from the active tenant context.

## Tenant administration

Global tenant administration is restricted to the system-level capability:

- `GET /api/admin/tenants`
- `POST /api/admin/tenants`
- `GET /api/admin/tenants/{tenant_id}/members`
- `POST /api/admin/tenants/{tenant_id}/members` with `{ "user_id": "...", "is_default": false }`
- `DELETE /api/admin/tenants/{tenant_id}/members/{user_id}`

Creating a tenant provisions the tenant registry row and its root organization
in one transaction. User membership is explicit; tenant IDs are never accepted
as arbitrary strings merely because they are non-empty.

Membership removal is soft-delete (`REMOVED`) for auditability. If the removed
member was the user's default tenant, IAM promotes the oldest remaining active
membership. This keeps the one-default invariant without coupling tenant
administration to Kratos credentials.

## Migration rules

The tenant migration registers the existing business tenant as
`arda-internal`, maps the known `default` placeholder to its fixed stable
ID (`00000000-0000-0000-0000-000000000010`), binds the root organization, and creates memberships. It intentionally does
not infer tenant ownership from an email address or organization name.

Historical data in other service databases must be inventoried and mapped to
the registered tenant before their local `default` values are removed. The
reserved `system` scope must remain outside business tenant data.

The first rollout includes explicit data migrations in CRM, Finance, HRM,
Notification, Platform and Workflow. They map only the legacy empty/default
placeholder to the fixed `arda-internal` ID and then validate the service's
tenant constraint. Media already required a non-empty tenant at table creation;
global Platform parameters, lookup catalogs and geographic reference data keep
`NULL` tenant scope by design.

Deployment order is: run the IAM migration and each service migration, verify
that no tenant-owned row has `default` or an empty tenant, then roll the
auth-gateway and domain services. A rollback is a database migration rollback,
not a runtime fallback: do not reintroduce `default` in application code.

Recommended verification queries per service are:

```sql
SELECT count(*) FROM <tenant_owned_table>
WHERE tenant_id IS NULL OR btrim(tenant_id) = '' OR lower(btrim(tenant_id)) = 'default';
```

The result must be zero for every tenant-owned table. Any tenant ID that is
not `arda-internal` must first exist in `iam_tenants` and have an explicit IAM
membership before data is exposed through the gateway.
