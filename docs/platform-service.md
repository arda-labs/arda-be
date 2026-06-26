# Platform Service

Last updated: 2026-06-27

## Purpose

`platform-service` owns shared platform data that does not belong to a single business module:

- system parameters
- lookup/combobox categories and values
- organization/unit/branch tree
- administrative geography
- later: sequences, feature flags, tenant settings

This service should not become a dumping ground. If data has a clear business owner, keep it in that business service.

## Data Ownership

| Data | Owner |
| --- | --- |
| users, roles, permissions | `iam-service` |
| auth/login/session orchestration | `auth-gateway` + `iam-service` |
| system parameters | `platform-service` |
| common lookups | `platform-service` |
| organizations, branches, departments | `platform-service` |
| administrative geography | `platform-service` |
| finance accounts/transactions | `finance-service` |

## Scope Model

Shared data that can vary by customer or org should use scope:

```txt
scope_type: global | tenant | org | branch | department
scope_id: null for global, otherwise the owning entity id
tenant_id: required for tenant-owned/customer data
```

Recommended lookup order for scoped parameters:

```txt
branch -> org -> tenant -> global
```

The first platform migration creates the scope columns, but fallback resolution should be implemented in the service layer when the first real consumer needs it.

## Administrative Geography

Administrative geography uses `geo_admin_units`, not fixed `province/district/ward` tables.

Reason: the Vietnamese administrative model changed after merger/reorganization, and future changes should not require a destructive schema redesign.

Reference context:

- Central direction moved local government toward a two-level model.
- Vietnam's post-reorganization administrative structure has 34 provincial-level units and 3,321 commune-level units.

Core fields:

```txt
code
name
full_name
parent_code
level
unit_type
effective_from
effective_to
is_active
metadata
```

Aliases/history are stored in `geo_admin_unit_aliases`.

## Current HTTP API

```txt
GET  /health/live
GET  /health/ready

GET  /api/platform/parameters
POST /api/platform/parameters

GET  /api/platform/lookups
POST /api/platform/lookups

GET  /api/platform/lookups/{category}/values
POST /api/platform/lookups/{category}/values

GET  /api/platform/organizations
POST /api/platform/organizations

GET  /api/platform/geo/admin-units
POST /api/platform/geo/admin-units
```

Current API is intentionally minimal. Update/delete, bulk import, fallback resolution, audit, and validation should be added as the admin UI becomes concrete.

## Database

Local default DSN:

```txt
postgres://arda_common:123456@localhost:5432/common?sslmode=disable
```

Docker Compose default DSN points to:

```txt
postgres://arda_common:123456@host.docker.internal:5432/common?sslmode=disable
```

The service also supports `DATABASE_DSN`.

## Next Steps

1. Add gRPC transport and proto contract for platform reads.
2. Add seed/import flow for Vietnamese administrative geography.
3. Add service-layer parameter fallback resolution.
4. Add audit events for platform changes.
5. Add admin UI screens for parameters, lookups, organizations, and geo data.
