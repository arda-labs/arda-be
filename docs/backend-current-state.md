# Backend Current State

Last updated: 2026-06-29

## Overview

Arda backend is a Go workspace with multiple services. The current runtime boundary is mostly HTTP/JSON, with Traefik and auth-gateway handling edge traffic and forward-auth.

Development dependencies are expected to run in the dev k3s/LAN environment rather than necessarily inside local Docker Compose:

- PostgreSQL
- Redis
- NATS
- Ory Hydra/Kratos

Current services:

| Service | Status | Responsibility |
| --- | --- | --- |
| `auth-gateway` | active | BFF/auth edge, session, Kratos/Hydra proxy, forward-auth |
| `iam-service` | active | users, roles, permissions, sessions, MFA, audit, login orchestration |
| `finance-service` | scaffolded/active | accounts, transactions, approvals |
| `platform-service` | scaffolded/active | system parameters, lookups, organizations, credit institutions, administrative geography |
| `mdm-service` | scaffold | placeholder service |

## Current Edge Flow

```txt
Frontend
  -> HTTP/JSON
  -> Traefik or auth-gateway
  -> service HTTP routes
```

Traefik dynamic routing currently includes:

- `/api/iam/public/*` -> `iam-service`, no forward-auth
- `/api/iam/*` -> `iam-service`, with forward-auth
- `/api/mdm/*` -> `mdm-service`, with forward-auth
- `/api/platform/*` -> `platform-service`, with forward-auth

auth-gateway also exposes auth/session routes and currently has a generic `/api/*` proxy. That proxy should not become the long-term service router for every service; service routing should remain explicit.

## Authorization State

The permission model uses IAM permissions plus Casbin policies.

Identity headers are standardized in `docs/auth-user-context-contract.md`.
The key rule is: `X-User-Id` is the internal IAM user UUID, while `X-User-Subject` is the external/Ory/Hydra subject.

Identity and credential ownership is documented in `docs/kratos-first-identity-design.md`.
The current direction is Kratos-first:

- Kratos owns identity traits and password credentials.
- Hydra owns OAuth2/OIDC login challenges, consent, and token issuance.
- IAM owns internal users, business profile, RBAC, sessions/devices, MFA policy, and audit.
- auth-gateway bridges Kratos, Hydra, IAM, and BFF browser sessions.

The browser login flow uses auth-gateway `/api/kratos/*` proxy routes and then
`/api/auth/kratos/accept-login` to accept the Hydra challenge with `iam_users.id`
as the Hydra subject.

Important current permissions:

- `superadmin`: sentinel wildcard permission for the system superadmin
- `platform.read`: read platform/reference data
- `platform.manage`: manage platform/reference data

## User And Identity State

Current admin user management routes are canonicalized under `/api/admin/users`:

- `GET/POST /api/admin/users`
- `GET/PUT/DELETE /api/admin/users/{id}`
- `PUT /api/admin/users/{id}/status`
- `POST /api/admin/users/{id}/identity/provision`
- `POST /api/admin/users/{id}/identity/password/reset`
- `GET/DELETE /api/admin/users/{id}/sessions`
- `GET /api/admin/identity/consistency`

Self-service profile and identity routes are intentionally split:

- IAM profile: `GET /api/iam/me`, `PUT /api/iam/me/profile`,
  `POST /api/iam/me/profile/avatar`, `POST /api/iam/me/profile/cover`
- Identity credentials: `PUT /api/identity/me/email`,
  `PUT /api/identity/me/password`

IAM runtime no longer uses legacy IAM password hashes for login. Password
authentication and password reset are managed by Kratos through IAM
`IdentityService`.

Route-level auth-gateway policy includes:

- `GET /api/platform/**` requires `platform.read`
- write methods on `/api/platform/**` require `platform.manage`

## Platform Service State

`platform-service` was added as the owner of shared platform/reference data:

- `plt_system_parameters`
- `plt_lookup_categories`
- `plt_lookup_values`
- `plt_organizations`
- `plt_areas`
- `plt_credit_institutions`
- `geo_admin_units`
- `geo_admin_unit_aliases`

Areas are modeled separately from administrative geography. `geo_admin_units`
captures legal/government structure, while `plt_areas` captures business or
operational grouping such as sales territories, service zones, or coverage
regions. Current managed fields include:

```txt
code
name
area_type_code
parent_id
admin_unit_code
description
status
effective_from
effective_to
```

Credit institutions are modeled separately from organizations because they are
shared reference entities with their own business identifiers and licensing
attributes. Current managed fields include:

```txt
code
name
address
status
effective_from
short_name
phone
email
license_no
license_date
tax_code
website
note
```

The administrative geography model is intentionally not hardcoded as `province -> district -> ward`. It uses a generic tree:

```txt
geo_admin_units
  code
  parent_code
  level
  unit_type
  effective_from
  effective_to
```

This supports the post-merger two-level local government model and keeps room for future administrative changes.

Reference context:

- Two-level local government model after administrative reorganization.
- 34 provincial-level units and 3,321 commune-level units after the 2025 reorganization.

## Verification State

Verified:

- `go test ./...` passes in `apps/platform-service`
- `go test ./...` passes in `apps/iam-service`
- `platform-service` compiles

Not verified locally:

- `platform-service` runtime migration from this Codex session, because direct `localhost:5432` was not reachable here. The intended dev DB can still be the k3s/LAN PostgreSQL endpoint configured through `DATABASE_DSN`.

## Cross-Cutting Concerns To Track

- gRPC for service-to-service communication.
- NATS for async events, cache invalidation, and future workflow/event use cases.
- Multilingual support through stable error codes, locale metadata, and translatable platform reference data.
- BPMN direction is Camunda 8.8+ / Zeebe with Arda workers and gRPC domain service calls; implementation should wait for Camunda 8.10 GA/stable.
