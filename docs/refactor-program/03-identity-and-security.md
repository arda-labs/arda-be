# Identity and Security Refactor Plan

## 1. Security invariants

1. Authentication proves who or what is calling.
2. Authorization evaluates an action against a target and verified scope.
3. Browser input never establishes identity, tenant, org, roles, permissions, or recent auth.
4. Unknown protected routes and unknown policies fail closed.
5. Self-service and management operations are modeled separately.
6. Internal network location is not service identity.
7. High-risk actions are auditable and may require recent primary auth or MFA.
8. Security context is explicit and immutable for the lifetime of one use-case execution.

## 2. Canonical authorization context

```text
principal
  kind: user | service | job
  id
  subject/provider identity when relevant

actor
  user/service that initiated the business action

target
  resource type and ID being accessed or changed

scope
  tenant_id
  org_ids or verified active_org_id
  ownership/relationship attributes

action
  stable capability such as iam.user.mfa.reset

assurance
  auth_time
  auth method/reference
  MFA/step-up state
  trusted-device state when applicable

delegation
  delegated_by, reason, expiry when acting on behalf of another principal
```

`actor` and `target` may be the same, but they are never assumed to be the same
for management endpoints.

## 3. Endpoint classes

### Self-service

Examples: profile, own sessions, own devices.

- Actor comes from the verified session.
- Target is bound to actor by the endpoint semantics.
- A body/path `user_id` cannot override target.
- Ownership checks still apply to subordinate resources such as sessions or files.

### Management

Examples: admin user update, target user session revoke, target MFA reset.

- Actor comes from the verified session.
- Target comes from the canonical resource path or validated body reference.
- Authorization checks action, tenant/org relationship, target attributes, separation
  of duties, and assurance level.
- Audit records actor, target, previous/new state summary, reason and correlation IDs.

### Internal workload

- Caller authenticates as a service/job identity.
- A delegated user actor is optional metadata and cannot replace workload identity.
- Callee authorizes both workload capability and delegated context where required.
- Background jobs never invent a user ID to pass user-only checks.

## 4. Route and policy registry

Create one generated/validated registry containing:

```text
method
path pattern
route owner
upstream service
public/internal classification
authentication requirement
permission/action
scope resolver
risk class
recent-auth/MFA requirement
rate-limit class
audit event
response profile
```

Required behavior:

- Every externally reachable route matches exactly one policy.
- Ambiguous or missing matches fail startup/CI and fail closed at runtime.
- Wildcards cannot silently expand across another service/domain.
- Ingress, Traefik and gateway route inventories are diff-checked.
- Policy tests include positive, negative, missing permission, wrong org, wrong
  tenant, stale auth, target mismatch and superadmin cases.
- Superadmin bypass, if retained, is explicit, audited and tested; it is not an empty permission set.

## 5. Tenant and organization resolution

The browser may request an organization selection, but the trusted context is
created only after membership and status validation.

Proposed flow:

```text
browser selects org
  -> gateway receives requested org ID
  -> IAM/Platform membership resolver validates actor + tenant + org + status
  -> gateway creates VerifiedScope
  -> internal metadata carries verified scope
  -> service rechecks resource belongs to that scope
```

Rules:

- Empty org membership is never interpreted as unrestricted access.
- `X-Org-Id` from public traffic is stripped before verified context injection.
- Tenant and org IDs use one canonical type and cannot default to `default` for scoped data.
- Cross-org management requires a separate explicit action, not a missing filter.
- Scope changes invalidate or version relevant session/cache context.

## 6. Gateway and downstream trust boundary

- Gateway strips all privileged context headers from public requests.
- Gateway injects signed or mutually authenticated internal context only after policy evaluation.
- Downstream services reject protected calls without an authenticated gateway/workload marker.
- Direct internal service exposure is restricted by network policy but does not rely on network policy alone.
- Request context parsing is centralized and rejects malformed IDs, oversized role lists, and inconsistent scope.

The service-identity ADR selects mTLS/SPIFFE, signed internal tokens, or another
supported mechanism. Plain headers over insecure gRPC are transitional only and
must have a removal work package.

## 7. Session and authorization freshness

- BFF sessions store normalized IAM user ID separately from external subject.
- Session authorization data has an `auth_version` or equivalent security stamp.
- Role, permission, suspension, password/MFA reset, and critical membership changes
  invalidate or force refresh of affected sessions.
- Low-risk reads may use bounded cached context; high-risk commands resolve fresh
  authorization context or verify current security stamp.
- Cache failure for security-critical decisions fails closed.

## 8. OAuth/OIDC/Kratos flow

Required target properties:

- Authorization Code with PKCE S256.
- Random, single-use, expiring state bound to the initiating browser session.
- Exact allowlisted redirect URIs.
- Locally verified issuer, audience, signature, expiry and nonce as applicable.
- No unguarded development code path in production builds/configuration.
- Legacy callback variants have explicit deprecation and removal.
- Token/session expiry derives from authoritative token/session lifetime.
- Refresh behavior, rotation, revocation and failure semantics are documented and tested.
- Cookies use the narrowest domain/path, `Secure`, `HttpOnly`, and appropriate `SameSite`.
- CSRF/origin checks cover all cookie-authenticated state-changing endpoints.

## 9. MFA, trusted device and recent auth

- MFA enrollment, verification, recovery, disable and reset are separate actions.
- Trusted device is evaluated through IAM policy and expiry, not mere record presence.
- IAM errors in trusted-device checks fail closed.
- Recent auth records a real authentication event and method; a generic confirmation
  boolean is not sufficient for high-risk production actions.
- Step-up response identifies required method and challenge without exposing secrets.
- FE suspends one operation, completes step-up once, and retries with the same logical request ID.
- Admin reset acts on a target user but authorization is based on the actor and target relationship.

## 10. Service-to-service authorization

Every internal client declares:

- workload identity;
- allowed destination and methods;
- deadline;
- retry class;
- delegated actor propagation rules;
- tenant/org propagation rules;
- audit responsibility.

Callees authorize service capability independently of end-user permission strings.
User permissions may be propagated for domain decisions, but are not proof that
the caller is an authorized service.

## 11. Audit model

Audit is append-only business/security evidence, separate from debug logs.

Minimum fields:

```text
event_id, occurred_at, action, outcome
actor_kind, actor_id, actor_subject
target_type, target_id
tenant_id, org_id
source_service, source_ip/device summary where permitted
reason, assurance method, auth_time
request_id, trace_id
safe change summary / policy decision reference
```

Do not record passwords, OTPs, tokens, cookies, raw authorization headers, full
presigned URLs, or unnecessary PII. Define retention, access control, export and
tamper-evidence requirements.

## 12. Security work order

Before broad contract refactoring:

1. Inventory and deny uncataloged public routes.
2. Strip and reconstruct privileged context at the edge.
3. Fix verified tenant/org resolution and empty-scope behavior.
4. Stabilize request ID and audit context.
5. Remove/disable production dev authentication branches.
6. Rotate exposed/tracked credentials and establish secret delivery.
7. Add authenticated workload identity for internal traffic.
8. Then migrate domain authorization endpoint by endpoint.

## 13. Security definition of done

- Route has exactly one registered policy and owner.
- Actor, target and scope are explicit in code and tests.
- Browser-controlled privileged headers are ineffective.
- Negative authorization tests outnumber the happy-path permission test where relevant.
- High-risk commands verify current assurance and produce audit.
- Internal caller identity is authenticated.
- Sensitive data is redacted from responses, logs and traces.
- Session invalidation/security-stamp behavior is tested.
- Rollback cannot reintroduce a known fail-open path.
