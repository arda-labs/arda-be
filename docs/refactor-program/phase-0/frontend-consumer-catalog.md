# Phase 0 Frontend API Consumer Catalog

Status: source inventory refreshed on 2026-08-25; migrated transport/auth
consumers are marked below and remaining protocol exceptions are explicit.

## Consumer inventory

| Consumer area | Current pattern | Target migration wave | Initial risk |
| --- | --- | --- | --- |
| `apps/shell/src/App.tsx` | shared `@workspace/auth` bootstrap and `/api/auth/me` adapter | FE-1/FE-4 | cookie/session regression is covered by transport test |
| `packages/auth/src/auth-guard.tsx` | normalized auth state/capabilities | FE-4 | no raw domain fetch remains |
| `packages/auth/src/ensure-recent-auth.ts` | shared API transport | FE-5 | request ID and single retry semantics |
| `packages/auth/src/oauth.ts` | explicit OAuth protocol adapter | FE-4/SEC-06 | provider-owned payload; credentials invariant required |
| `packages/auth/src/pages.tsx` | shared transport plus isolated Kratos protocol calls | FE-4/SEC-06 | raw calls are not domain API clients |
| `packages/auth/src/step-up.tsx` | shared API transport | FE-5 | recent-auth state and retry semantics |
| `packages/auth/src/store.ts` | shared API transport | FE-4 | session lifecycle consistency |
| `packages/api/src/client.ts` | canonical shared JSON transport | foundation owner | credentials, request ID, idempotency, abort and problem parsing |
| `packages/media/src/index.ts` | shared media API but mixed FormData/result shapes | PILOT-03 | upload profile and durable media ID |
| `apps/iam/**/api.ts` | domain API adapters | DOM-IAM | admin target semantics and error profiles |
| `apps/platform/**/api.ts` | domain API adapters | PILOT-01/DOM-PLT | arrays vs list objects and filters |
| `apps/finance/**/api.ts` | domain API adapters | DOM-FIN | money, command idempotency and state |
| `apps/crm/**/api.ts` | domain API adapters | DOM-CRM | tenant/org scope and workflow commands |
| `apps/hrm/**/api.ts` | domain API adapters | DOM-HRM | PII and lifecycle commands |
| `apps/workflow/**/api.ts` | large adapter plus workbench/draft helpers | DOM-WFL | legacy list/query shapes, Zeebe operation risk |
| `apps/account/**` | self-service adapters | DOM-IAM | actor=self and session/security behavior |
| `packages/notifications/**` | inbox/realtime consumers | DOM-NOT | SSE/session expiry/reconnect |

## Raw transport locations

These are not all defects; some auth/Kratos or binary flows may remain explicit
protocol adapters. The remaining raw calls are approved OAuth/Kratos protocol
adapters; the generic JSON client is the only shared transport and the static
credential gate enforces `credentials: "include"`:

```text
arda-mfe/packages/auth/src/oauth.ts
arda-mfe/packages/auth/src/pages.tsx
arda-mfe/packages/api/src/client.ts
```

## Current response assumptions to verify

- Some list consumers expect `T[]`.
- Some consumers expect `{ items?: T[] }`.
- Some platform consumers expect resource arrays while backend routes may return
  list objects or different pagination fields.
- Some action consumers expect `{ ok: boolean }`.
- Some calendar/API consumers expect nested `data` while others expect a flat object.
- Workflow adapters contain legacy `limit`/`size` style query usage.
- Auth/session payloads intentionally use a separate compatibility shape only
  for Kratos/OAuth provider payloads; `/api/auth/me` uses the canonical success
  envelope and auth consumers unwrap `result` explicitly.

The exact field-level matrix is the next GOV-02 output. This document records the
existence of mixed assumptions so migration agents do not infer one shape from a
single successful page.

## Required GOV-02 expansion

For every `api.ts`, raw transport call and generated/client wrapper, record:

```text
consumer_id, file, exported function/hook, operation/path/method,
request params/body, current wire type, actual response fixture,
error handling, query key, invalidation, actor/target, scope,
success profile target, problem codes, migration wave, owner, status
```

## FE pilot candidates

### Read list

Platform organizations or IAM permissions. Selection must be based on:

- bounded list size;
- no destructive mutation in the pilot;
- clear owner and policy;
- ability to test empty/list/error/forbidden states;
- response-shape mismatch visible enough to validate the adapter.

### Management command

Admin MFA reset or another target-user command. It must wait for the security
decision and use actor/target/recent-auth/audit tests.

### Media

Avatar/cover or a representative file flow with upload-init/complete/download
semantics. It must not persist temporary presigned URLs as durable profile state.

## FE catalog completion gate

- [x] All raw fetches classified as shared JSON transport or explicit auth
  protocol adapter; `check:credentials` enforces `credentials: "include"`.
- [ ] All domain API modules have operation owners and response fixtures.
- [ ] Query keys and URL state are recorded for list features.
- [ ] Auth/session consumers are separated from domain consumers.
- [ ] Shell/remotes/shared packages have version compatibility owners.
