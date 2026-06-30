# Kratos-first identity flow

Last updated: 2026-06-29

## Summary

Arda now treats Ory Kratos as the source of truth for identity and credentials.
IAM keeps the internal user record and business profile, while Hydra remains the
OAuth2/OIDC authorization server.

Responsibilities are intentionally split:

- Kratos owns identity traits, password credentials, recovery, verification,
  browser settings, and identity lifecycle.
- Hydra owns OAuth2/OIDC authorization challenges, consent, token issuance, and
  client-facing OAuth semantics.
- IAM owns `iam_users`, profile fields, tenants, RBAC, sessions/devices, MFA
  policy, audit, and admin user management.
- auth-gateway owns browser-facing BFF sessions, Kratos public flow proxying,
  Hydra accept-login/accept-consent, and request context forwarding.

## Canonical identifiers

Use these identifiers for one purpose only:

| Field | Owner | Meaning |
| --- | --- | --- |
| `iam_users.id` | IAM | Internal Arda user UUID. This is the canonical subject for business authorization. |
| `iam_users.kratos_identity_id` | IAM cache / Kratos link | Kratos identity ID used for Kratos Admin API calls. |
| `iam_identity_mappings.external_id` | IAM mapping | External provider subject. For provider `kratos`, this mirrors `kratos_identity_id`. |
| Hydra subject | Hydra/auth-gateway | Accepted login subject. Arda uses `iam_users.id`. |

Do not use `external_subject` for new Kratos calls.

## Data ownership

Kratos identity traits should contain login/identity-facing data:

- `email`
- `username`
- `name`
- `first_name`
- `last_name`
- optional display/profile traits when they are needed by Kratos UI flows

IAM business profile contains product-facing profile and authorization data:

- `display_name`
- `nickname`
- `first_name`
- `last_name`
- `gender`
- `country`
- `address`
- `position`
- `avatar_file_id`
- `picture_url`
- `cover_file_id`
- `cover_image_url`
- `tenant_id`
- roles, permissions, organizations, devices, sessions, audit records

IAM may cache identity-facing values such as `email`, `username`, and
`display_name` for list/search UX. Writes to credential-owned fields must go
through `IdentityService`.

## Login flow

Browser login uses a Kratos API flow through auth-gateway. The gateway strips
browser-only headers when proxying the API flow to Kratos so Kratos does not
reject requests with the API-vs-browser CSRF guard.

```txt
Browser
  -> GET /api/kratos/login/api
  -> POST /api/kratos/login?flow=<flow_id>
  -> Kratos validates identifier/password
  -> Browser calls POST /api/auth/kratos/accept-login with login_challenge
  -> auth-gateway calls Kratos whoami
  -> auth-gateway resolves Kratos identity through IAM
  -> IAM returns internal user context
  -> auth-gateway accepts Hydra login with iam_users.id as subject
  -> auth-gateway creates BFF session and records IAM session metadata
```

Important rules:

- Frontend must not call Kratos public URLs directly. It uses auth-gateway
  `/api/kratos/*` routes.
- Browser calls must use the matching Kratos flow type. Do not submit an API
  flow directly to Kratos with browser `Origin` or `Cookie` headers.
- `Kratos whoami` responses may be compressed. Gateway must decode them before
  JSON parsing.
- Hydra login subject is the internal IAM user ID, not the Kratos identity ID.

## User management flow

Admin user management is owned by IAM. Kratos operations are accessed only
through IAM `IdentityService`.

Supported admin operations:

| Operation | Route |
| --- | --- |
| List/create users | `GET/POST /api/admin/users` |
| Get/update/delete user | `GET/PUT/DELETE /api/admin/users/{id}` |
| Enable/disable user | `PUT /api/admin/users/{id}/status` |
| Provision Kratos identity | `POST /api/admin/users/{id}/identity/provision` |
| Reset Kratos password | `POST /api/admin/users/{id}/identity/password/reset` |
| Audit identity mappings | `GET /api/admin/identity/consistency` |
| List/revoke user sessions | `GET/DELETE /api/admin/users/{id}/sessions` |
| Assign/unassign roles | `POST /api/admin/users/{id}/roles`, `DELETE /api/admin/users/{id}/roles/{roleId}` |

Create/edit user supports the profile fields currently surfaced in the admin UI:

- `username`
- `email`
- `firstName`
- `lastName`
- `nickname`
- `gender`
- `country`
- `address`
- `position`
- `tenantId`
- `status`

Password reset only works for users that have a linked Kratos identity. If a
legacy/internal user has no `kratos_identity_id`, provision or link the identity
first.

## Self-service profile and identity

Self-service business profile routes:

- `GET /api/iam/me`
- `PUT /api/iam/me/profile`
- `POST /api/iam/me/profile/avatar`
- `POST /api/iam/me/profile/cover`

Self-service identity routes:

- `PUT /api/identity/me/email`
- `PUT /api/identity/me/password`

Keep the separation:

- Profile edits update IAM business fields.
- Credential/identity edits go through Kratos via IAM `IdentityService`.

## Session and device flow

auth-gateway creates and validates BFF sessions for browser traffic. IAM stores
session/device metadata for management, audit, and limits.

Current user routes:

- `GET /api/iam/me/sessions`
- `DELETE /api/iam/me/sessions/{id}`
- `DELETE /api/iam/me/sessions?keep=<current_session_id>`
- `GET /api/iam/me/devices`
- `DELETE /api/iam/me/devices/{id}`
- `POST /api/iam/me/devices/{id}/trust`

Internal gateway-to-IAM routes:

- `POST /internal/iam/sessions`
- `DELETE /internal/iam/sessions/{id}`
- `GET /internal/iam/users/by-kratos-identity/{identityId}/context`
- `POST /internal/iam/users/resolve-kratos-identity`
- `POST /internal/iam/users/resolve-identity`

## Service boundary

All Kratos Admin API calls go through `service.IdentityService`.

Handlers and business services must not call `kratos.Client` directly. The
identity service is responsible for:

- creating/provisioning Kratos identities
- linking Kratos identities to IAM users
- resolving `kratos_identity_id`
- updating email and synchronizing the IAM cache
- updating/resetting passwords through Kratos
- deleting/deactivating identity state when user lifecycle needs it
- converting Kratos HTTP errors into domain errors

## Frontend contract

Frontend auth flow:

- `features/auth/login/page.tsx` creates and submits Kratos login API flows
  through `/api/kratos/*`.
- After Kratos validates credentials, frontend calls
  `/api/auth/kratos/accept-login` to complete the Hydra challenge.
- Auth store is hydrated from normalized `/api/auth/me` user context.

Frontend admin user management:

- `features/iam/api.ts` calls canonical `/api/admin/users/*` routes.
- The users table keeps many row actions behind a dropdown menu.
- Create/edit dialogs use fixed header/footer with a scrollable body.

## Cleanup status

Completed:

- Runtime no longer uses IAM `password_hash` for login.
- Kratos password reset resolves identity by `iam_users.kratos_identity_id` or
  Kratos identity mapping.
- Legacy `/api/iam/me/profile/email` route was removed.
- Admin user routes are canonical `/api/admin/users/*`.
- auth-gateway strips browser headers from Kratos API login proxy calls.
- `whoami` decode handles compressed Kratos responses.

Still track:

- Decide when to physically drop legacy password columns if all environments are
  migrated.
- Keep `iam_identity_mappings` audit command available while legacy users exist.
- Add end-to-end tests for login, provision identity, reset password, and admin
  edit user.
