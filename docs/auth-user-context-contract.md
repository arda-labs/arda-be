# Auth User Context Contract

Last updated: 2026-06-29

## Purpose

This document defines the stable identity context passed from the edge auth layer to Arda backend services.

The most important rule:

```txt
X-User-Id is always the internal IAM user UUID.
X-User-Subject is the upstream auth subject kept for traceability.
```

Services must not guess whether `X-User-Id` is an OAuth subject, username, email, or provider id.

## Header Contract

| Header | Required | Meaning |
| --- | --- | --- |
| `X-User-Id` | yes for user requests | Internal IAM `iam_users.id` UUID. This is the canonical user key inside Arda. |
| `X-User-Subject` | yes when available | Upstream auth subject kept for traceability. In the current Kratos-first login bridge, Hydra is accepted with `iam_users.id`, so this may equal `X-User-Id`. It is not a database foreign key. |
| `X-Username` | optional | Display/login username from IAM context. |
| `X-User-Email` | optional | User email from IAM context. |
| `X-Tenant-Id` | yes for tenant-scoped requests | Current tenant id from IAM context. |
| `X-Roles` | optional | Comma-separated role codes. |
| `X-Permissions` | optional | Comma-separated permission codes. |
| `X-Auth-Version` | yes for user requests | IAM security stamp. It increases when user security, roles, or effective permissions change. |
| `X-Auth-Time` | BFF session requests | Unix timestamp for the last primary authentication event, used for recent-auth checks. |
| `X-Auth-Checked` | yes for protected routes | Set to `true` by auth-gateway after auth has been evaluated. |

For future gRPC calls, these headers become lowercase metadata keys:

```txt
x-user-id
x-user-subject
x-username
x-user-email
x-tenant-id
x-roles
x-permissions
x-auth-version
x-auth-time
x-auth-checked
```

## Resolution Flow

The edge auth layer is responsible for translating token/session data into internal IAM context.

```txt
Token/session or Kratos identity
  -> auth-gateway
  -> IAM user context lookup
       1. by Kratos identity ID during login bridge
       2. by internal id for existing BFF/Hydra sessions
       3. by legacy external subject only as compatibility fallback
  -> inject X-User-* headers
  -> downstream service
```

Downstream services should treat the injected headers as the request identity and should not call Hydra/Kratos directly for normal request context.

## Service Rules

Self-service endpoints must use `X-User-Id` as the actor/user id.

Examples:

```txt
POST /api/iam/me/profile/avatar
GET  /api/iam/me/sessions
POST /api/media/files/init-upload
```

Rules:

- Do not trust `user_id` from request body for self-service operations.
- Do not use `X-User-Subject` as a database foreign key.
- Store `X-User-Id` in audit records as the actor id.
- Store `X-User-Subject` only when provider-level traceability is needed.
- Background/service-account calls without a user must use service identity metadata instead of fake user ids.

## Avatar/Profile Media Flow

User avatar is a cross-service flow:

```txt
FE
  -> media-service init-upload
  -> Garage/S3 presigned PUT
  -> media-service complete-upload
  -> IAM POST /api/iam/me/profile/avatar
```

IAM persists:

```txt
iam_users.avatar_file_id
iam_users.picture_url
```

`avatar_file_id` is the durable media reference.

`picture_url` is optional and can be used for external provider images. Do not persist short-lived presigned download URLs in IAM because they expire.

## Gateway Responsibilities

`auth-gateway` must:

- Resolve user context through IAM before creating a BFF session.
- Store `session.User.UserID` as internal IAM UUID only.
- Store `session.User.Subject` as external/Ory/Hydra subject.
- Forward `X-User-Id`, `X-User-Subject`, tenant, roles, and permissions on proxied API calls.
- Return the same normalized user context from `/api/auth/me`.

## Compatibility Note

During migration, a Hydra subject may already be an internal IAM UUID for password login flows. Gateway supports this by trying IAM lookup by subject first and by internal id second.

This fallback belongs only in auth-gateway. New service code should depend on the normalized `X-User-Id` header.

With the current Kratos-first bridge, newly accepted Hydra login requests should
use `iam_users.id` as the Hydra subject. Kratos identity IDs are stored on the IAM
user and in identity mappings; they should not be used as `X-User-Id`.
