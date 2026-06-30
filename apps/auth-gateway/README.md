# auth-gateway

`auth-gateway` is Arda's browser-facing auth edge and BFF.

## Responsibilities

- Traefik forward-auth endpoint: `GET /auth/check`
- Kratos public flow proxy: `/api/kratos/*`
- Hydra bridge: Kratos-authenticated login accept, consent accept, and token exchange
- Browser BFF session creation, validation, logout, and header forwarding
- IAM context resolution before protected requests reach downstream services

## Kratos + Hydra login flow

```txt
Frontend
  -> GET /api/kratos/login/api
  -> POST /api/kratos/login?flow=<flow_id>
  -> POST /api/auth/kratos/accept-login
  -> Kratos whoami
  -> IAM resolve/link by Kratos identity
  -> Hydra accept login with iam_users.id
  -> BFF session cookie
```

Important behavior:

- The API login proxy strips browser-only headers before forwarding to Kratos API
  flows. This avoids Kratos rejecting API flows as browser/CSRF requests.
- `whoami` responses can be compressed and must be decoded before JSON parsing.
- Gateway stores the internal IAM user ID in session user context.

## Main routes

| Route | Purpose |
| --- | --- |
| `GET /auth/check` | ForwardAuth check for Traefik |
| `POST /api/auth/kratos/accept-login` | Complete Kratos-authenticated Hydra login challenge |
| `POST /api/auth/accept-consent` | Accept Hydra consent |
| `POST /api/auth/callback` | Exchange authorization code with Hydra |
| `GET /api/kratos/whoami` | Proxy Kratos whoami |
| `GET /api/kratos/login/api` | Create Kratos API login flow |
| `GET /api/kratos/login/browser` | Create Kratos browser login flow |
| `GET /api/kratos/login/flows` | Fetch Kratos login flow |
| `POST /api/kratos/login` | Submit Kratos login flow |
| `GET/POST /api/kratos/settings*` | Proxy Kratos settings flows |
| `GET/POST /api/kratos/recovery*` | Proxy Kratos recovery flows |
| `GET/POST /api/kratos/verification*` | Proxy Kratos verification flows |
| `GET /api/auth/me` | Return normalized current BFF user context |
| `POST /api/auth/logout` | Revoke current BFF session |
| `GET /api/auth/me/sessions` | List BFF sessions for current user |

## Context forwarding

Protected proxied requests receive:

- `X-User-Id`: internal IAM user UUID
- `X-User-Subject`: external/Ory subject, for traceability
- `X-Username`
- `X-User-Email`
- `X-Tenant-Id`
- `X-Roles`
- `X-Permissions`
- `X-Auth-Checked: true`

See `../../docs/auth-user-context-contract.md` for the stable contract.
