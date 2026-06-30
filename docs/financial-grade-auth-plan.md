# Financial-grade auth plan

Last updated: 2026-06-30

## Goal

Arda's current auth shape is a good enterprise baseline:

```txt
Browser/API client
  -> Traefik/auth-gateway
  -> Kratos for identity and credentials
  -> Hydra for OAuth2/OIDC
  -> IAM for internal users, tenants, RBAC, sessions, devices, and audit
```

For finance-oriented products, the goal is not to add more services. The goal is
to make auth resilient, auditable, and risk-aware while keeping stale security
decisions tightly bounded.

## Principles

- `iam_users.id` remains the canonical Arda user id.
- Downstream services trust only gateway-injected auth context.
- External client-supplied `X-User-*`, `X-Roles`, `X-Permissions`,
  `X-Session-Id`, and `X-Auth-Checked` headers must be stripped at the edge.
- Login may depend on Kratos, Hydra, IAM, and the session store.
- Normal authenticated API requests should avoid per-request dependency on
  Hydra when a locally verifiable JWT is available.
- Cached IAM context is allowed only with short TTLs and explicit invalidation
  paths.
- High-risk operations must use fresh authorization context and recent
  authentication.

## Route risk levels

Use route risk to decide whether cached context is acceptable.

| Level | Examples | Required behavior |
| --- | --- | --- |
| `low` | read profile, dashboard, static metadata | JWT local verify, short IAM context cache allowed |
| `medium` | update profile, export data, create business records | JWT local verify, fresh context when cache is stale |
| `high` | transfer funds, approve payout, change password/email/MFA, add beneficiary, grant admin roles | fresh IAM check, recent auth, step-up MFA when configured |

Policy files should eventually carry this risk level per route. Until then,
admin/security routes should be treated as `high` by code or convention.

## Token policy

Target production policy:

- Access tokens are JWTs with short lifetime, normally `5m-15m`.
- Gateway verifies JWT locally using issuer JWKS:
  - allowed algorithms only;
  - required `iss`, `aud`, `exp`, `nbf`;
  - refresh JWKS on unknown `kid`.
- Refresh tokens use rotation.
- Emergency revocation uses one or more of:
  - browser session revocation in Valkey;
  - `jti` denylist for active access tokens;
- user/session `auth_version` bump.

Introspection remains useful for opaque tokens, emergency checks, or high-risk
flows, but should not be required for every normal API request.

## IAM context policy

Gateway may cache normalized IAM user context for normal routes:

- default TTL: `30s-60s`;
- critical route TTL: `0s-5s` or fresh lookup;
- cache key: internal user id or token subject after normalization;
- cached fields: user id, tenant id, roles, permissions, status, auth version;
- never cache a negative or ambiguous identity resolution for long.

Invalidation events should be emitted when:

- user is disabled/deleted;
- password, email, MFA, or recovery settings change;
- role or permission assignments change;
- sessions/devices are revoked;
- tenant membership changes.

## Session policy

Browser session state is authoritative in Valkey/Sentinel for BFF traffic.

Recommended controls:

- absolute session timeout;
- inactivity timeout;
- device id and fingerprint tracking;
- session revoke by id and revoke all except current;
- `auth_version` stored in session and compared during context refresh;
- session snapshot fallback allowed only for non-critical routes and only for a
  bounded stale window.

## Step-up policy

High-risk actions should require a recent authentication event:

```txt
recent_auth_at >= now - 5m
```

If not recent, the frontend should trigger re-auth or MFA before retrying the
operation. The gateway/service should enforce the requirement server-side.

## Audit policy

Audit every security-relevant event:

- login success/failure;
- token exchange failure;
- logout/session revoke;
- user disable/delete;
- role/permission changes;
- password/email/MFA changes;
- high-risk business action approval.

Audit records should include actor id, tenant id, session id, device id, IP,
user agent, route/action, result, and correlation/request id.

## Implementation order

1. Strip external auth context headers before proxying to internal services.
2. Add route risk metadata to gateway policy.
3. Add JWKS-based local JWT verifier and make it the production default for
   normal protected routes.
4. Add short-lived IAM context cache in auth-gateway.
5. Add `auth_version`/security stamp and cache invalidation on IAM changes.
6. Enforce recent-auth and MFA step-up for high-risk routes.
7. Add structured security audit events and dashboards.

## Current status

- Done: gateway strips client-supplied auth context headers in the BFF proxy.
- Done: gateway policy supports route risk metadata and ForwardAuth emits
  `X-Auth-Risk`.
- Done: auth-gateway supports `TOKEN_STRATEGY=jwks` with `JWKS_URL` for local
  RS256 JWT verification.
- Done: legacy direct Hydra accept-login endpoint was removed; login accept now
  requires Kratos whoami and IAM subject resolution.
- Done: ForwardAuth caches IAM user context for non-high-risk routes with a
  short configurable TTL. High-risk routes always resolve fresh IAM context.
- Done: IAM exposes `authVersion` / `X-Auth-Version` and bumps it for user,
  password, role, and permission changes.
- Done: BFF sessions store primary auth time and forward `X-Auth-Time` for
  downstream recent-auth checks.
- Done: BFF proxy enforces recent-auth for `risk: high` routes using
  `recent_auth_window_seconds`.
- Done: BFF proxy enforces matched route auth and permissions instead of only
  forwarding best-effort user headers.
- Next: make gateway cache/session invalidation compare cached auth version
  against token/session auth version when version claims are added.
