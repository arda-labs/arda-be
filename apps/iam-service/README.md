# iam-service

`iam-service` owns Arda internal users, profile data, RBAC, sessions/devices,
MFA policy, audit, and identity orchestration.

## Identity model

Arda is Kratos-first for identity and credentials:

- Kratos owns identity traits and password credentials.
- IAM owns internal user records, business profile, authorization, session
  metadata, and audit.
- IAM stores `iam_users.kratos_identity_id` to link an internal user to a Kratos
  identity.
- All Kratos Admin API operations go through `service.IdentityService`.

Do not add direct `kratos.Client` calls in handlers or unrelated services.

## Superadmin bootstrap

`iam-service` always reconciles the canonical `superadmin@arda.local` IAM user
and `SUPER_ADMIN` role on startup. To also ensure the Kratos identity can log in,
set `SUPERADMIN_INITIAL_PASSWORD` on the `iam-service` deployment.

For the current k3s dev cluster:

```bash
kubectl -n platform create secret generic iam-service-secrets \
  --from-literal=SUPERADMIN_INITIAL_PASSWORD=admin123 \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n platform set env deploy/iam-service \
  --from=secret/iam-service-secrets

kubectl -n platform rollout restart deploy/iam-service
```

Use a real secret value outside dev. Leaving `SUPERADMIN_INITIAL_PASSWORD` unset
keeps existing superadmin credentials untouched.

## Admin user management

| Route | Purpose |
| --- | --- |
| `GET /api/admin/users` | List users |
| `POST /api/admin/users` | Create IAM user and optional Kratos identity |
| `GET /api/admin/users/{id}` | Get one user |
| `PUT /api/admin/users/{id}` | Edit user/profile fields |
| `DELETE /api/admin/users/{id}` | Delete/deactivate user |
| `PUT /api/admin/users/{id}/status` | Enable or disable user |
| `POST /api/admin/users/{id}/identity/provision` | Create/link Kratos identity for an existing user |
| `POST /api/admin/users/{id}/identity/password/reset` | Reset Kratos password |
| `GET /api/admin/identity/consistency` | Audit IAM/Kratos identity mappings |
| `GET /api/admin/users/{id}/sessions` | List user sessions |
| `DELETE /api/admin/users/{id}/sessions` | Revoke user sessions |

Admin create/edit accepts profile fields used by the current frontend:

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

Password reset requires a linked Kratos identity. For legacy users without
`kratos_identity_id`, provision identity first.

## Self-service routes

Business profile routes:

- `GET /api/iam/me`
- `PUT /api/iam/me/profile`
- `POST /api/iam/me/profile/avatar`
- `POST /api/iam/me/profile/cover`

Identity/credential routes:

- `PUT /api/identity/me/email`
- `PUT /api/identity/me/password`

Session/device routes:

- `GET /api/iam/me/sessions`
- `DELETE /api/iam/me/sessions/{id}`
- `DELETE /api/iam/me/sessions?keep=<current_session_id>`
- `GET /api/iam/me/devices`
- `DELETE /api/iam/me/devices/{id}`
- `POST /api/iam/me/devices/{id}/trust`

## Internal gateway routes

auth-gateway uses these routes to resolve user context and record sessions:

- `GET /internal/iam/users/by-id/{id}/context`
- `GET /internal/iam/users/by-kratos-identity/{identityId}/context`
- `POST /internal/iam/users/resolve-kratos-identity`
- `POST /internal/iam/sessions`
- `DELETE /internal/iam/sessions/{id}`

## Related docs

- `../../docs/kratos-first-identity-design.md`
- `../../docs/auth-user-context-contract.md`
