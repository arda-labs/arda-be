# Phase 0 Endpoint and Edge Catalog

Status: draft generated from route source inspection on 2026-08-25.

This is a route-family inventory, not yet the final operation catalog. Handlers
that dispatch several methods internally must be expanded into one row per
operation before Phase 1 policy enforcement. `inspect` means the method/policy
must be confirmed from handler and gateway behavior.

## Catalog fields

```text
operation_id, method, path, owner, upstream, visibility, actor, target, scope,
action, risk, recent_auth, idempotency, success_profile, error_profile,
audit, source, status
```

## Edge and authentication routes

| Path/family | Methods | Owner/upstream | Visibility | Initial classification | Source/status |
| --- | --- | --- | --- | --- | --- |
| `/api/auth/login` | GET | auth-gateway | public | auth initiation | auth-gateway router / catalog |
| `/api/auth/start` | GET | auth-gateway | public | OAuth initiation | auth-gateway router / catalog |
| `/api/auth/callback` | GET/POST | auth-gateway | public callback | OAuth callback; high security | auth-gateway router + BFF / inspect |
| `/api/auth/kratos/accept-login` | POST | auth-gateway | public/session | login bridge | auth-gateway router / inspect |
| `/api/auth/accept-consent` | POST | auth-gateway | session | consent | auth-gateway router / inspect |
| `/api/auth/me` | GET | auth-gateway | session | current session resource; canonical success/problem profile | auth-gateway router + `contracts/openapi/auth-v1.json` / migrated |
| `/api/auth/logout` | POST | auth-gateway | session | session command | auth-gateway router / catalog |
| `/api/auth/step-up` | POST | auth-gateway | session | high-risk assurance command | auth-gateway router / pilot candidate |
| `/api/auth/recent-auth` | GET | auth-gateway | session | assurance status | auth-gateway router / catalog |
| `/api/auth/me/sessions` | GET | auth-gateway | session | self-service list | auth-gateway router / catalog |
| `/api/kratos/**` | GET/POST | auth-gateway -> Kratos | public/session | identity flow proxy | auth-gateway router / inspect |
| `/api/` | all | auth-gateway proxy | mixed | generic proxy; P0 security review | BFF `Proxy` / critical |

## IAM and admin routes

| Path/family | Methods | Actor/target | Initial classification | Source/status |
| --- | --- | --- | --- | --- |
| `/api/admin/users` | GET/POST | admin -> collection | list/create management | IAM router / inspect |
| `/api/admin/users/{id}` | GET/PUT/DELETE | admin -> target user | management resource | IAM router / inspect |
| `/api/admin/users/{id}/status` | PUT | admin -> target user | high-risk command | IAM router / inspect |
| `/api/admin/users/{id}/identity/**` | POST | admin -> target identity | high-risk management | IAM router / inspect |
| `/api/admin/users/{id}/mfa/reset` | POST | admin -> target user | high-risk MFA command | IAM router / pilot candidate |
| `/api/admin/users/{id}/sessions` | GET/DELETE | admin -> target sessions | management list/command | IAM router / inspect |
| `/api/admin/users/{userId}/roles/**` | POST/DELETE | admin -> target user/role | RBAC command | IAM router / inspect |
| `/api/admin/groups/**` | GET/POST/PUT/DELETE | admin -> group/member/role | RBAC management | IAM router / inspect |
| `/api/admin/roles/**` | GET/POST/PUT/DELETE | admin -> role/permission | RBAC management | IAM router / inspect |
| `/api/admin/permissions/**` | GET/DELETE | admin -> permission | permission catalog/management | IAM router / read pilot candidate |
| `/api/admin/policies/**` | GET/POST | admin -> policy | policy management | IAM router / high-risk inspect |
| `/api/admin/audit/**` | GET | admin -> audit collection | audit read/verify | IAM router / inspect |
| `/api/admin/identity/consistency` | GET | admin -> identity state | diagnostic query | IAM router / inspect |
| `/api/iam/me` | GET | self | profile resource | IAM router / catalog |
| `/api/iam/me/profile` | PUT | self | profile command | IAM router / catalog |
| `/api/iam/me/profile/avatar` | POST | self | media reference command | IAM router / media flow |
| `/api/iam/me/profile/cover` | POST | self | media reference command | IAM router / media flow |
| `/api/iam/me/sessions/**` | GET/DELETE | self | session list/command | IAM router / catalog |
| `/api/iam/me/devices/**` | GET/DELETE/POST | self | trusted-device management | IAM router / high-risk inspect |
| `/api/iam/me/mfa/**` | GET/POST | self | MFA lifecycle | IAM router / high-risk inspect |
| `/api/identity/me/**` | PUT | self | credential change | IAM router / high-risk inspect |

## Platform routes

| Path/family | Methods | Initial classification | Source/status |
| --- | --- | --- | --- |
| `/api/platform/public/branding` | GET | public reference | Platform router / catalog |
| `/api/platform/parameters/**` | GET/POST/PUT/DELETE | reference management | Platform router / inspect |
| `/api/platform/lookups/**` | GET/POST/DELETE | reference management | Platform router / inspect |
| `/api/platform/lookup-values/**` | GET/POST/PUT/DELETE | reference management | Platform router / inspect |
| `/api/platform/organizations/**` | GET/POST/PUT/DELETE | tenant/org management | Platform router / pilot dependency |
| `/api/platform/geo/admin-units` | GET | large reference list | Platform router / catalog |
| `/api/platform/credit-institutions/**` | GET/POST/PUT/DELETE | reference management | Platform router / inspect |
| `/api/platform/areas/**` | GET/POST/PUT/DELETE | scoped reference management | Platform router / inspect |
| `/api/platform/templates/**` | GET/POST/PUT/DELETE | template management | Platform router / inspect |
| `/api/platform/calendar/**` | GET/POST | domain command/query | Platform router / inspect |

## Business routes

| Owner | Path/family | Initial risks |
| --- | --- | --- |
| CRM | `/api/crm/customers/**` | tenant/org scope, target ownership, workflow command |
| CRM legacy | `/api/v1/customers` | removed; no supported FE consumer found |
| HRM | `/api/hrm/positions/**`, `/job-titles/**`, `/org-units/**` | org scope, tree query, reference consistency |
| HRM | `/api/hrm/employees`, `/employee-registrations/**` | PII, lifecycle, submit command |
| Finance | `/api/finance/accounts/**`, `/trial-balance` | tenant, money, read consistency |
| Finance | `/api/finance/incoming-transactions/**`, `/outgoing-transactions/**` | idempotency, duplicate command |
| Finance | `/api/finance/transactions/**`, `/transactions/search` | state transitions, tenant, money |
| Finance | `/api/finance/approvals/**` | actor/target, state transition, audit |
| Finance | `/api/finance/accounting/**` | reference ownership and read profile |
| Media | `/api/media` and `/api/media/**` | upload, visibility, tenant/owner authorization |
| Workflow | `/api/workflow/cases/**`, `/case-types/**` | tenant, idempotency, Zeebe boundary |
| Workflow | `/api/workflow/process-definitions/**` | XML/binary, deployment risk, audit |
| Workflow | `/api/workflow/tasks/**`, `/work-items/**` | actor/assignment, state transition |
| Workflow | `/api/workflow/roles/**`, `/delegations/**` | delegated actor and scope |
| Workflow | `/api/workflow/operate/**` | high-risk operational control |
| Workflow legacy | `/api/v1/workflows/**` | removed; no supported FE consumer found |
| Notification | HTTP health only in router; internal command/event paths | service identity and event delivery |
| MDM | HTTP health only in current cmd router | ownership/exposure confirmation |

## Edge inventory actions

1. Expand every handler-dispatch row into method-level operations.
2. Compare this list with `arda-be/apps/auth-gateway/configs/policy.yaml`.
3. Compare with `arda-infra/k8s/apps/auth-gateway-ingress.yaml` and Traefik routes.
4. Compare with MFE consumers and deployed gateway access logs.
5. Assign stable operation IDs, policy/action, success/error profile and audit class.
6. Mark route families as canonical, compatibility, public, internal-only or remove-candidate.

## Initial gaps requiring owner validation

- Generic `/api/` proxy must not authorize an unknown route by session presence alone.
- Finance, media and workflow policy coverage must be checked against actual route inventory.
- Legacy `/api/v1` route families are removed; keep route-policy and gateway
  negative tests to prevent accidental reintroduction.
- Route source handlers often accept multiple methods; method-level policy must be explicit.
- Public and internal service exposure must be distinguished from browser route presence.
