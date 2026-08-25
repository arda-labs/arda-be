# Phased Execution Plan

Durations below are sizing ranges, not delivery commitments. A phase exits by
evidence, not by elapsed time. Security containment may run immediately after its
Phase 0 decisions even while non-blocking inventory work continues.

## Dependency map

```text
Phase 0: baseline, catalogs, ADRs
  ├── Phase 1: security containment
  └── Phase 2: contract and platform foundations
          └── Phase 3: three pilot vertical slices
                  ├── Phase 4: IAM + Platform migration wave
                  ├── Phase 5: CRM + HRM + Finance migration wave
                  └── Phase 6: Workflow + Media + Notification migration wave
                          └── Phase 7: cross-domain data/integration hardening
                                  └── Phase 8: operational hardening and scale validation
                                          └── Phase 9: legacy removal and closure
```

Phases 4-6 can overlap by domain only after Phase 3 proves the shared foundations.
Within a domain, FE and BE move in the same vertical slice.

## Phase 0 — Program setup and baseline

Indicative size: 1-2 weeks.

### Objectives

- Turn the audit into current, machine-readable catalogs.
- Approve architecture decisions and migration constraints.
- Establish behavior, security, performance and operations baselines.
- Select pilots and prepare non-overlapping implementation packages.

### Workstreams

1. Endpoint/policy and ingress route catalog.
2. FE consumer/raw-fetch/response-shape/query-key catalog.
3. Table/schema/query/tenant and PII catalog.
4. Synchronous call and event topology catalog.
5. Existing tests, dashboards, alerts and runbook inventory.
6. ADR-001 through ADR-010 review.
7. Error-code, permission/action and resource-name registry draft.
8. Baseline API/public-origin/internal timings, DB plans and MFE load metrics.
9. Credential exposure inventory and private rotation schedule.
10. Select three pilots:
    - read-only paginated list;
    - high-risk management command with actor != target;
    - media upload/complete/download path.

### Deliverables

- Approved charter and named owners.
- Catalogs and migration ledger.
- Accepted or explicitly deferred ADRs with owners/dates.
- Baseline report and stored reproducible test commands.
- Phase 1-3 task packets.

### Exit gate

- No externally reachable route is absent from the inventory.
- Known audit findings are linked to work packages.
- Pilot owners and success/rollback measures are approved.
- Current dirty source changes are accounted for before implementation starts.

## Phase 1 — Security containment

Indicative size: 1-3 weeks; begins before broad behavior refactoring.

### Objectives

Close fail-open and trust-boundary risks without requiring full response migration.

### Workstreams

1. Deny unknown gateway policy routes and validate route registry coverage.
2. Make Traefik/gateway/domain routing explicit for all services.
3. Strip privileged browser headers and inject verified context only.
4. Validate tenant/org membership and remove empty-scope unrestricted behavior.
5. Repair request ID stability for policy/error/audit paths.
6. Remove or production-disable legacy OAuth/dev callback shortcuts.
7. Rotate exposed/tracked secrets and establish secret references.
8. Add transitional downstream verification that protected calls came through an
   authenticated gateway while full workload identity is prepared.
9. Add negative auth tests for actor/target/scope/recent-auth cases.

### Compatibility rule

Response bodies may stay legacy in this phase if changing them would expand risk.
The security behavior, status code, request ID, and audit outcome must still be testable.

### Exit gate

- Unknown routes fail closed.
- Forged org/user/permission headers do not affect authorization.
- Management actions distinguish actor and target.
- High-risk routes have recent-auth behavior and audit tests.
- Compromised/development credentials are rotated or disabled.
- Rollback cannot restore the known fail-open behavior.

## Phase 2 — Contract and shared foundations

Indicative size: 2-4 weeks.

### Objectives

Build the minimum stable primitives used by every subsequent slice.

### Backend workstreams

- OpenAPI domain layout, lint, generation and breaking-change pipeline.
- RFC 9457 problem type/error registry and HTTP response profiles.
- Stable request/trace context middleware.
- Typed verified auth/scope context.
- List/cursor query parsers with allowlisted filter/sort.
- gRPC client/server interceptors for identity, context, deadline and status.
- Event envelope and outbox/inbox reference implementation.
- Idempotency reference store/handler.

### Frontend workstreams

- One `@workspace/api` transport for JSON auth/domain calls.
- Generated contract consumption and domain adapter pattern.
- Typed `ApiProblem`, field-error mapping and i18n code registry.
- Stable request ID across step-up retry.
- Standard offset/cursor list hooks, query keys and URL mapping.
- Upload/download/SSE adapters.
- Contract fixtures and compatibility tests.
- Federation shared-dependency/manifest compatibility check.

### Infrastructure/testing workstreams

- OTel propagation and collector/export baseline.
- Structured logs and redaction.
- Integration stack for Postgres, JetStream, Redis and service identity.
- CI route-policy, contract, Proto/event breaking and secret gates.
- Feature switch/canary pattern.

### Exit gate

- Foundation libraries are documented, independently tested and version-compatible.
- A reference service and reference MFE fixture prove the contract without a live domain migration.
- Existing consumers are unaffected until a slice opts in.
- Generated outputs are reproducible and CI detects drift.
- Foundation APIs have no domain-specific business DTOs.

## Phase 3 — Pilot vertical slices

Indicative size: 3-5 weeks.

### Pilot A: read-only paginated admin list

Recommended candidate: permissions or another bounded reference list after Phase 0
confirms low mutation risk.

Validates:

- OpenAPI response and problem profiles;
- policy/verified scope;
- generated FE adapter;
- URL filters, pagination and backend total;
- request/trace IDs, metrics and query plans;
- old/new compatibility and canary.

### Pilot B: high-risk management command

Recommended candidate: an admin action against a target user, selected only after
the Phase 1 auth model is secure.

Validates:

- actor != target;
- capability, tenant/org scope and target rules;
- real recent-auth/step-up;
- audit outcome;
- idempotency/concurrency if applicable;
- FE confirmation, suspended mutation and typed error behavior.

### Pilot C: media lifecycle

Validates:

- JSON upload session;
- direct binary/multipart transfer;
- upload completion and durable resource;
- tenant/org/owner/visibility authorization;
- event/outbox processing;
- download/range/caching behavior;
- expiry and abandoned-upload cleanup.

### Exit gate

- All three profiles meet the cross-stack definition of done.
- Canary and rollback are rehearsed, not hypothetical.
- Measured latency/error/query/bundle results stay within budget.
- Shared foundations needed no domain-specific fork.
- Lessons are folded back into ADRs/templates before scaling.

## Phase 4 — IAM and Platform migration wave

Indicative size: 4-8 weeks, delivered in small slices.

### IAM sequence

1. Public session/auth contract adapters.
2. Self-service `/me` profile, sessions, devices and security.
3. Admin users.
4. Groups, roles, permissions and assignments.
5. MFA/trusted device/recovery/reset.
6. Audit query/export.
7. Internal IAM context/permission contracts.
8. Remove legacy callback/client paths after compatibility evidence.

### Platform sequence

1. Organizations and verified membership/reference usage.
2. Parameters and lookup categories/values.
3. Areas and administrative geography.
4. Credit institutions and remaining shared references.
5. Platform internal gRPC consumers and cache invalidation strategy.

### FE scope

Migrate IAM, account and platform remotes feature-by-feature. Correct the auth
guard organization response mismatch and remove fail-open capability behavior.

### Exit gate

- All IAM/Platform public routes use registered policy and target contract.
- `/me` and `/admin/{target}` semantics are unambiguous.
- IAM/Platform FE calls use the standard transport/adapters.
- Session/security changes invalidate authorization context correctly.
- Old shapes have telemetry-backed zero or approved remaining consumers.

## Phase 5 — CRM, HRM and Finance migration wave

Indicative size: 6-12 weeks, parallel by domain after shared prerequisites.

### CRM

- Customer/member list, detail and command slices.
- Verified org scope with explicit empty-scope denial.
- Search/index strategy and large-list pagination.
- Workflow submission through typed contract and idempotency.

### HRM

- Position/job title/org unit reference slices.
- Employee lifecycle and management target rules.
- PII classification, field-level exposure and audit.
- Workflow interactions and projections.

### Finance

- Tenant-scoped account and balance reads.
- Transaction create/post/reverse and approval state machines.
- Idempotency scoped by tenant + operation + key.
- Same-tenant ledger/account invariants.
- Concurrency, decimal/money and reconciliation tests.
- Incoming/outgoing operation queues and search.

### FE scope

Each domain remote moves alongside its endpoint slices. Finance command UI cannot
switch until idempotency and state-transition tests are ready.

### Exit gate

- Cross-tenant negative tests cover every migrated aggregate.
- Finance invariants reconcile before/after migration.
- Domain/workflow calls are typed, authenticated, deadline-bound and traced.
- No migrated FE feature parses a service-specific legacy error shape.

## Phase 6 — Workflow, Media and Notification migration wave

Indicative size: 5-10 weeks.

### Workflow

- Case/process/task/SLA contracts and tenant-safe repositories.
- Preserve public idempotency key through gRPC and repository layers.
- Move all Zeebe access under Workflow service.
- Authenticated worker-to-domain commands.
- Incident/retry/suspend/resume and visible operation state.

### Media

- Complete upload/download/visibility lifecycle migration.
- Outbox publisher, processing state, replay and cleanup.
- Domain attachment ownership contracts.

### Notification

- Authenticated internal command path.
- Durable JetStream publish with acknowledgement.
- Inbox/dedupe, retry, dead-letter and template/data classification.
- SSE/inbox frontend behavior and reconnect/session expiry.

### Exit gate

- No domain service connects directly to Zeebe.
- Event loss/redelivery/replay scenarios pass integration tests.
- Media get/delete/download authorization is scope-safe.
- Notification delivery is observable from originating event to terminal outcome.

## Phase 7 — Cross-domain data and integration hardening

Indicative size: 3-8 weeks; can start per-domain after its migration wave.

### Objectives

- Align remaining identifier/tenant/time/money types.
- Add constraints and tenant-first indexes after data cleanup.
- Backfill and validate old rows.
- Evaluate and pilot PostgreSQL RLS defense-in-depth.
- Complete service workload identity and encryption in transit.
- Remove insecure/ad-hoc internal HTTP clients.
- Establish event replay, DLQ operations and projection rebuild procedures.

### Exit gate

- Catalog reports no unowned tables or unscoped tenant repository methods.
- Constraints validate against all production data.
- Workload identity covers every protected internal call.
- Restore/replay/rebuild procedures have evidence.

## Phase 8 — Operational hardening and scale validation

Indicative size: 2-5 weeks plus observation window.

### Workstreams

- SLO burn-rate alerts and domain dashboards.
- End-to-end production-like performance and capacity tests.
- Failure injection: dependency timeout, redelivery, stale session, partial rollout.
- Backup restore and disaster recovery rehearsal.
- MFE current/previous remote compatibility deployment exercise.
- Security review, secret rotation rehearsal and log-redaction validation.
- Runbooks and on-call ownership.

### Exit gate

- Critical journey SLOs meet approved targets.
- Capacity results include achieved throughput and saturation.
- Rollback/forward recovery and restore/replay are rehearsed.
- No critical/high unresolved security finding in the migrated surface.

## Phase 9 — Legacy removal and program closure

Indicative size: 2-6 weeks after compatibility windows expire.

### Workstreams

- Remove legacy response parsers/adapters and raw fetch paths.
- Remove old API versions/routes and temporary proxy behavior.
- Remove legacy internal HTTP clients, insecure transport and duplicate DTOs.
- Drop old columns/tables only after final validation and backup retention approval.
- Remove feature switches and dual-write/read code.
- Update existing convention/current-state/roadmap documents to the final truth.
- Archive migration dashboards while retaining operational SLOs.
- Produce final architecture, decision and residual-risk report.

### Exit gate

- Migration ledger has no unowned temporary compatibility item.
- Production telemetry confirms no supported consumer uses removed contracts.
- Documentation matches runtime source and deployed manifests.
- Program owner accepts residual risks and hands ownership to normal domain operations.

## Phase gate rules

- A failed gate blocks dependent work, not unrelated safe catalog/test work.
- A waiver names owner, risk, expiry and compensating control.
- Near-complete is not complete when the missing item is authorization, data
  integrity, rollback, secret handling or event durability.
- Phase status is evidence-linked in the migration ledger.
