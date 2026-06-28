# Kratos-first identity design

## Goal

Arda keeps Kratos as the source of truth for identity and credentials, while IAM owns business profile, RBAC, tenants, organizations, sessions, MFA policy, and audit.

The current mixed model stores some authentication state in IAM and some identity state in Kratos. This creates ambiguous identifiers and sync bugs. The target model makes each responsibility explicit:

- Kratos owns identity traits, password credentials, recovery, verification, browser settings, and identity lifecycle.
- Hydra owns OAuth2/OIDC authorization, login challenges, consent, and token issuance.
- IAM owns the internal user record, business profile, roles, permissions, tenants, organizations, device/session tracking, and audit.
- auth-gateway owns browser-facing BFF sessions, Kratos public flow proxying, and Hydra/Kratos/IAM bridging.

## Canonical identifiers

Use three different identifiers and never overload one field for another meaning:

- `iam_users.id`: Arda internal user ID. This is the canonical subject for business authorization and Hydra login acceptance.
- `iam_users.kratos_identity_id`: Kratos identity ID. This is the only identifier used with Kratos Admin API.
- `iam_identity_mappings.external_id`: External provider subject. For Kratos it mirrors `kratos_identity_id`; for future social/OIDC federation it can store provider subjects.

`external_subject` is legacy and must not be used for new Kratos calls.

## Data ownership

Kratos traits:

- `email`
- `name`
- optional login/display traits such as `username`, `first_name`, `last_name`

IAM business profile:

- `display_name`, `nickname`, `avatar_file_id`, `picture_url`, `cover_file_id`, `cover_image_url`
- `tenant_id`, org membership, roles, permissions
- `department`, `position`, `employee_id`, `approval_level`, `daily_limit`, `bio`
- device/session/audit records

IAM can cache identity-facing values like `email` and `display_name` for reads, but writes to identity-owned fields must go through `IdentityService`.

## Service boundary

All Kratos Admin API calls go through `service.IdentityService`.

Handlers and business services must not call `kratos.Client` directly. The identity service is responsible for:

- creating Kratos identity and IAM identity mapping
- resolving `kratos_identity_id` for an internal user
- updating email and synchronizing the IAM cache
- updating/resetting password through Kratos
- deleting/deactivating identity during user deletion or suspension
- hiding Kratos HTTP details behind domain errors

## Target routes

Self-service identity:

- `GET /api/identity/me`
- `PUT /api/identity/me/email`
- `PUT /api/identity/me/password`
- `POST /api/identity/recovery`
- `POST /api/identity/verification`

Self-service business profile:

- `GET /api/iam/me`
- `PUT /api/iam/me/profile`
- `POST /api/iam/me/profile/avatar`
- `POST /api/iam/me/profile/cover`

Admin user management:

- `GET /api/admin/users`
- `POST /api/admin/users`
- `GET /api/admin/users/{id}`
- `PUT /api/admin/users/{id}`
- `DELETE /api/admin/users/{id}`
- `PUT /api/admin/users/{id}/identity/email`
- `POST /api/admin/users/{id}/identity/password/reset`
- `POST /api/admin/users/{id}/status`
- `GET /api/admin/users/{id}/sessions`
- `DELETE /api/admin/users/{id}/sessions`
- `POST /api/admin/users/{id}/roles`
- `DELETE /api/admin/users/{id}/roles/{roleId}`

Legacy routes may stay temporarily as wrappers while the frontend migrates.

## Login bridge

Target login flow:

1. Browser enters Hydra login challenge.
2. auth-gateway starts or forwards a Kratos login flow.
3. Kratos authenticates the browser and sets its session.
4. auth-gateway calls Kratos `whoami`.
5. auth-gateway asks IAM to resolve `kratos_identity_id` to `iam_users.id`.
6. IAM returns full user context.
7. auth-gateway accepts Hydra login using `iam_users.id` as subject.
8. auth-gateway creates its BFF session and IAM session tracking record.

## Migration plan

1. Add nullable `iam_users.kratos_identity_id`.
2. Backfill it from `iam_identity_mappings` where `provider_id = 'kratos'`.
3. For legacy rows where `source = 'kratos'` and `external_subject` contains a Kratos identity ID, backfill from `external_subject`.
4. Route all new writes through `IdentityService`.
5. Dual-read `kratos_identity_id` first, then legacy mapping/subject during transition.
6. Add consistency audit command before dropping legacy columns.
7. Remove direct Kratos calls from handlers and business services.
8. Remove IAM password authentication once Kratos login bridge is complete.

## Done criteria

- No handler calls `kratos.Client` directly.
- No service uses `external_subject` as a Kratos identity ID.
- Create, update email, reset password, disable/delete identity all go through `IdentityService`.
- Hydra subject is consistently `iam_users.id`.
- IAM no longer needs `password_hash` for normal login.
- Frontend identity routes are separate from IAM business profile routes.
