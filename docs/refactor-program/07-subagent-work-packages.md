# Subagent Work Packages

This document is the backlog structure for implementation agents. It is not an
instruction to start coding. Package scopes and file paths must be refreshed from
the target branch immediately before assignment.

## 1. Assignment rules

- One agent owns one work package at a time.
- One package should normally fit one reviewable PR and one deployable concern.
- Foundation contracts merge before consumer packages start.
- Avoid two agents editing the same shared package, Proto/OpenAPI definition,
  gateway policy registry, or migration file concurrently.
- Domain FE and BE packages may run in parallel only after the operation contract
  is frozen and each agent has disjoint files.
- Agent handoff includes diff, tests, unresolved risks, migration/rollback and catalog updates.
- Agents do not modify unrelated dirty files.

## 2. Package status vocabulary

```text
draft -> ready -> in_progress -> review -> merged -> deployed_canary
      -> observed -> compatibility_complete -> legacy_removed
```

`blocked` requires an explicit dependency/decision, not merely incomplete work.

## 3. Governance and inventory

| ID | Package | Depends on | Primary output |
| --- | --- | --- | --- |
| GOV-01 | Endpoint, ingress and gateway policy catalog | none | Machine-readable route catalog and gaps |
| GOV-02 | FE API consumer and raw-fetch catalog | none | Operation-to-feature matrix |
| GOV-03 | Database/schema/query/PII catalog | none | Table ownership and invariant matrix |
| GOV-04 | Internal call and event topology catalog | none | Caller/callee/topic/deadline/auth map |
| GOV-05 | Decision register and ADR-001..010 | GOV-01..04 | Approved architecture decisions |
| GOV-06 | Migration ledger and compatibility inventory | GOV-01..05 | Per-slice migration state |
| GOV-07 | Baseline report | GOV-01..04 | Correctness, latency, DB, MFE and ops baseline |

Safe parallel group: GOV-01 through GOV-04 and baseline collection, with one
integrator for terminology and IDs.

## 4. Security foundations

| ID | Package | Depends on | Primary output |
| --- | --- | --- | --- |
| SEC-01 | Gateway route registry and deny-by-default matching | GOV-01, ADR-004 | Fail-closed route enforcement + coverage tests |
| SEC-02 | Privileged header stripping and verified context | ADR-003/004 | Trusted gateway context boundary |
| SEC-03 | Tenant/org membership resolver | GOV-03, ADR-003 | VerifiedScope and negative tests |
| SEC-04 | Actor/target authorization primitives | ADR-003 | Self/admin/internal target model |
| SEC-05 | Session auth-version/freshness policy | SEC-04 | Freshness and invalidation behavior |
| SEC-06 | OAuth callback/PKCE/state/token hardening | ADR-003 | Production-safe login/callback flow |
| SEC-07 | MFA/trusted-device/recent-auth contract | SEC-04/05 | Real assurance + FE step-up behavior |
| SEC-08 | Internal workload identity reference | ADR-006, OPS-02 | Authenticated gateway/gRPC reference path |
| SEC-09 | Audit schema and sensitive-data rules | ADR-009, SEC-04 | Append-only audit contract and redaction |
| SEC-10 | Credential rotation and tracked-config remediation | none | Rotated secrets and safe references |

Serialization rule: SEC-01/02/03 contract changes are integrated by one owner or
merged sequentially. SEC-06 and SEC-07 may proceed separately after auth context is stable.

## 5. API and backend foundations

| ID | Package | Depends on | Primary output |
| --- | --- | --- | --- |
| API-01 | OpenAPI layout/lint/generation | ADR-001/002/005 | Reproducible public contract pipeline |
| API-02 | Success response profiles | API-01 | Shared schemas and conformance fixtures |
| API-03 | RFC 9457 problem/error registry | API-01 | Stable errors and HTTP mapping |
| API-04 | Pagination/filter/sort contract | API-01/02 | Offset/cursor schemas and parser rules |
| API-05 | Upload/download/operation/batch profiles | API-01/02/03 | Non-basic operation contracts |
| BE-01 | Stable request/trace context in `arda-http` | ADR-009 | One logical request ID and trace propagation |
| BE-02 | HTTP writer/problem mapper | API-02/03, BE-01 | Reference boundary implementation |
| BE-03 | Typed auth/scope application context | SEC-02/03/04 | Shared technical context primitive |
| BE-04 | Reference service layering | BE-02/03 | Transport -> application -> repository example |
| BE-05 | Proto generation/breaking pipeline | ADR-006 | Reproducible internal contracts |
| BE-06 | gRPC client/server interceptors | BE-01/03/05, SEC-08 | Identity, deadline, trace, status middleware |
| BE-07 | Idempotency reference implementation | ADR-008 | Scoped command replay semantics |
| BE-08 | Event/outbox/inbox reference | ADR-007, BE-01 | Durable acknowledged event flow |

API-01 must merge before API-02 through API-05. BE-01 is a high-contention shared
package and should have one owner until its public API stabilizes.

## 6. Frontend foundations

| ID | Package | Depends on | Primary output |
| --- | --- | --- | --- |
| FE-01 | Unified JSON transport | API-01/03, BE-01 behavior | Credentials, IDs, abort, decode, typed problem |
| FE-02 | Generated-client/domain-adapter pattern | API-01/02 | Reference generated consumer |
| FE-03 | Error/i18n/field mapping | API-03, FE-01 | One `ApiProblem` UI behavior |
| FE-04 | Auth bootstrap and fail-closed capability checks | SEC-04/05, FE-01 | Stable shell/remotes auth state |
| FE-05 | Step-up suspended-request retry | SEC-07, FE-01/04 | Single prompt, preserved logical request |
| FE-06 | Offset/cursor list/query-key standard | API-04, FE-01/02 | Shared hooks and fixtures |
| FE-07 | Upload/download/SSE adapters | API-05, FE-01 | Protocol-specific transports |
| FE-08 | Federation compatibility/version manifest gate | ADR-002 | Shell/remote compatibility checks |
| FE-09 | Browser telemetry and error boundary standard | OBS-01, FE-01 | Route/remote/API telemetry |
| FE-10 | Frontend test harness and contract fixtures | FE-01/02/03 | Unit/component/browser contract tests |

FE-01 is serialized with auth raw-fetch migration. Feature agents consume FE-01;
they do not add behavior directly to the transport.

## 7. Data and integration foundations

| ID | Package | Depends on | Primary output |
| --- | --- | --- | --- |
| DATA-01 | Database standards and schema linter/checklist | ADR-008, GOV-03 | Accepted type/invariant standard |
| DATA-02 | Scoped repository interface and test harness | DATA-01, BE-03 | Cross-tenant negative test reference |
| DATA-03 | Expand/backfill/validate tooling pattern | DATA-01 | Restart-safe migration reference |
| DATA-04 | RLS feasibility pilot | DATA-02/03 | Evidence and adopt/reject ADR amendment |
| INT-01 | Deadline/retry budget matrix | GOV-04, BE-06 | Per-call policy registry |
| INT-02 | Service identity rollout | SEC-08, BE-06, OPS-02 | Authenticated internal calls |
| INT-03 | Event schema registry | ADR-007, GOV-04 | Versioned event catalog |
| INT-04 | JetStream stream/consumer/DLQ policy | BE-08, OPS-02 | Durable delivery configuration |
| INT-05 | Replay/projection rebuild tooling | INT-03/04 | Controlled recovery procedure |

## 8. Observability, quality and operations foundations

| ID | Package | Depends on | Primary output |
| --- | --- | --- | --- |
| OBS-01 | W3C trace propagation and OTel baseline | ADR-009 | Browser-to-service/event trace |
| OBS-02 | Structured log and redaction schema | ADR-009, SEC-09 | Safe searchable logs |
| OBS-03 | Metrics/SLO/dashboard baseline | GOV-07, OBS-01 | Critical journey and dependency dashboards |
| OBS-04 | Audit storage/query/retention implementation plan | SEC-09 | Operational audit lifecycle |
| QA-01 | Characterization test suite | GOV-01/02 | Current behavior safety net |
| QA-02 | OpenAPI/consumer contract gate | API-01, FE-02 | Producer-consumer compatibility CI |
| QA-03 | Auth policy matrix test harness | SEC-01/04 | Positive and negative policy matrix |
| QA-04 | DB migration/concurrency test harness | DATA-02/03 | Production-like migration tests |
| QA-05 | Event redelivery/replay test harness | BE-08, INT-04 | Delivery correctness tests |
| QA-06 | Performance suite and budgets | GOV-07, OBS-03 | Repeatable public/internal/MFE load tests |
| OPS-01 | Secret delivery and rotation | SEC-10 | Runtime secret references/runbook |
| OPS-02 | Integration/staging runtime parity | GOV-04 | Real dependencies and service identity testbed |
| OPS-03 | Feature switch/canary/rollback pattern | ADR-010, OBS-03 | Reference safe rollout |
| OPS-04 | Backup/restore and event replay drill | DATA-03, INT-05 | Recovery evidence |
| OPS-05 | GitOps policy/rendered-manifest CI | OPS-01/02 | Validated environment changes |

## 9. Pilot packages

Exact resource names are selected in Phase 0.

| ID | Package | Depends on | Scope |
| --- | --- | --- | --- |
| PILOT-01 | Paginated read slice | API/BE/FE list foundations, SEC-01..04, QA-02/03 | FE + gateway + service + DB + telemetry |
| PILOT-02 | High-risk target command | SEC-04/05/07/09, BE-07, FE-05 | Actor/target/recent-auth/audit/idempotency |
| PILOT-03 | Media lifecycle slice | API-05, FE-07, BE-08, INT-04 | Upload/complete/event/download/access |
| PILOT-04 | Pilot canary and retrospective | PILOT-01..03, OPS-03, QA-06 | Rollout evidence and foundation amendments |

Do not assign PILOT packages until their operation contracts and policy rows are frozen.

## 10. Domain migration epics

Each epic is decomposed into resource/use-case slices using the task template.

| ID | Epic | Mandatory prerequisites |
| --- | --- | --- |
| DOM-IAM | IAM/account/auth sessions, users, RBAC, MFA, audit | Phase 1, PILOT-04, FE-04/05 |
| DOM-PLT | Organizations and platform reference data | Phase 1, PILOT-04, DATA-02 |
| DOM-CRM | Customers/workbench and workflow submit | SEC-03, PILOT-04, BE-06/07 |
| DOM-HRM | HR references and employee lifecycle | SEC-03, PILOT-04, PII rules |
| DOM-FIN | Accounts, ledger, transactions, approvals | DATA-02, BE-07, concurrency/reconciliation harness |
| DOM-WFL | Cases, definitions, task/SLA, Zeebe boundary | BE-06/07/08, INT-02/04 |
| DOM-MED | Media authorization, lifecycle and processing | PILOT-03, INT-04 |
| DOM-NOT | Notification commands, events, inbox/SSE | BE-08, INT-02/04, FE-07 |

## 11. Example slice decomposition

For `admin users list`, create separate but linked packages only after contract freeze:

1. `IAM-USERS-LIST-CONTRACT`: OpenAPI operation, errors, policy row, fixtures.
2. `IAM-USERS-LIST-BE`: service/repository/response and integration tests.
3. `IAM-USERS-LIST-FE`: generated adapter, query/list UI and component tests.
4. `IAM-USERS-LIST-OPS`: dashboard/canary/compatibility switch if nontrivial.
5. `IAM-USERS-LIST-CLEANUP`: remove old adapter after observation window.

The contract package merges first. BE and FE then run in parallel. Cleanup never
shares the initial migration PR.

## 12. Agent handoff requirements

Every completed package reports:

- exact files changed and why;
- contract/policy/catalog entries changed;
- commands and test results;
- unrun tests with reason;
- data migration and compatibility state;
- security/PII implications;
- telemetry/dashboard evidence;
- rollback or forward-recovery steps;
- remaining temporary code and its cleanup package ID.

## 13. Execution snapshot — 2026-08-25

The following foundation packages have landed in the current worktree and have
passed their available compile/unit/static gates: `GOV-01..07`, `SEC-01..04`,
`SEC-07`, source-side `SEC-10`, `API-01..05`, `BE-01..03`, `BE-06..08`,
`FE-01..10`, `DATA-01..03`, `INT-02`, `INT-03`, `INT-04`
(publisher/consumer reference), and the domain scope waves
for IAM, CRM, HRM, Finance, Platform, Workflow, Media and Notification.

Still `in_progress` rather than complete: `SEC-05/06/09`, `BE-04`, `DATA-04`,
`OBS-01..04`, `QA-02/04..06`, and `OPS-01..05`. `INT-05` now has a bounded
operator CLI for one-event DLQ replay with explicit confirmation and audited
operator identity; live replay/projection-drill evidence remains open.
`BE-05` now has a
canonical seven-source Proto tree, reproducible generation and a content-drift
CI gate. `INT-01` now has a machine-readable nine-call deadline/retry/identity
policy and CI validation. Source-side secret hygiene, browser telemetry, event
registry, media contract and interaction-policy checks are landed; the
remaining portions require runtime, provider, consumer-generation, recovery or
environment evidence that is not available from the current local kubeconfig.

`API-05` now has a machine-readable media contract for multipart upload,
attachment, binary view and redirect download profiles. `FE-08` now has a
repository gate checking shell/remote registration, shared singleton drift,
stable remote entries, versions and fixed ports; deployed compatibility still
requires the shell/remote matrix and immutable artifact evidence.
`INT-03` now has a version-1 event registry and a CI consistency check against
the shared Go subject/event-code constants; payload publication and enabled
consumer evidence remain separate requirements.

The IAM target-scope slice and Finance/Workflow request-hash conflict semantics
were extended after the initial snapshot; their service-level Go tests and
static gates pass, while deployment/runtime evidence remains open under the
same packages.

Workflow role membership target scope and task claim tenant predicates have also
landed. Delegations are now tenant-scoped end to end; historical delegation
rows still require explicit data assignment before the new constraint can be
validated. Role catalog, assignment rules and process definitions are explicitly
classified as service-global rather than receiving an artificial tenant.
