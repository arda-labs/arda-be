# Arda Cross-Stack Refactor Program

Status: execution in progress — foundations are being migrated incrementally.

Last updated: 2026-08-25

## Purpose

This folder is the canonical execution plan for the next large Arda refactor.
It covers the browser-facing MFE, auth-gateway, domain services, databases,
service-to-service communication, events, observability, testing, infrastructure,
deployment, and rollback.

The program is intentionally organized by cross-stack vertical slices. A migrated
capability is not considered complete when only its backend or frontend has moved.
It is complete when its route, policy, contract, UI consumer, persistence,
telemetry, tests, rollout, and rollback path satisfy the same definition of done.

## Why this program exists

The current codebase has useful shared building blocks, but runtime behavior is
not yet as uniform as the existing convention documents suggest. Important gaps
found during the 2026-08 audit include:

- public routes that can reach the generic auth-gateway proxy without a matching
  policy;
- organization context forwarded from browser input without a single verified
  membership resolution path;
- mixed success and error response shapes across services;
- raw frontend `fetch` calls alongside the shared API client;
- request IDs that can be regenerated within one logical request;
- repository operations that do not consistently include tenant scope;
- internal HTTP/gRPC calls without a complete workload identity contract;
- events and outbox records without a uniform delivery, acknowledgement, and
  deduplication model;
- existing documents that describe some migrations as complete while exceptions
  remain in source.

This plan treats those findings as a baseline and is now being executed through
small, tested vertical slices. A code change is valid only when it is linked to a
work package, uses an explicit versioned/protocol compatibility boundary where
required (never runtime shape guessing), and records verification.

## Program outcomes

At completion, Arda should have:

1. One catalog of public and internal endpoints, each with an owner and policy.
2. Explicit actor, target, tenant, organization, delegation, and risk semantics.
3. Versioned HTTP, gRPC, event, and upload contracts with compatibility checks.
4. One frontend transport path and generated or contract-checked types.
5. Tenant-safe repository and schema invariants enforced in code and database.
6. Authenticated service-to-service calls, deadlines, idempotency, and safe retry.
7. End-to-end request and trace correlation, structured logs, audit, metrics, and SLOs.
8. Characterization, contract, security, migration, and performance test gates.
9. Expand-migrate-contract deployments with canary, rollback, and data recovery.
10. Small work packages that can be assigned independently to coding agents.

## Document map

| Document | Scope |
| --- | --- |
| [01-architecture-and-governance.md](01-architecture-and-governance.md) | Charter, target boundaries, ownership, decisions, compatibility |
| [02-contracts-and-frontend.md](02-contracts-and-frontend.md) | HTTP profiles, errors, OpenAPI, FE transport/query/form/upload standards |
| [03-identity-and-security.md](03-identity-and-security.md) | Actor/target model, gateway policy, org scope, OAuth, MFA, service identity |
| [04-data-and-service-integration.md](04-data-and-service-integration.md) | Database standards, query scope, migrations, gRPC, events, idempotency |
| [05-observability-testing-and-operations.md](05-observability-testing-and-operations.md) | Telemetry, audit, SLOs, test pyramid, secrets, deployment controls |
| [06-phased-execution-plan.md](06-phased-execution-plan.md) | Phases, dependencies, entry/exit gates, domain migration waves |
| [07-subagent-work-packages.md](07-subagent-work-packages.md) | Assignable work packages and safe parallelization |
| [08-templates-and-checklists.md](08-templates-and-checklists.md) | Task packet, endpoint catalog, ADR, migration and definition-of-done templates |

Phase 0 working package:

- [Phase 0 baseline, catalogs and ADR register](phase-0/README.md)
- [Implementation progress and verification](implementation-progress.md)

Companion plans:

- [Arda MFE refactor plan](../../../arda-mfe/docs/refactor-program.md)
- [Arda infrastructure refactor plan](../../../arda-infra/docs/refactor-program.md)

## Sources of truth and precedence

During planning, this program describes the proposed target and migration path.
It does not silently override runtime behavior or older convention documents.

After the Phase 0 decision gates are approved, precedence is:

1. accepted ADR;
2. published OpenAPI, Proto, or event schema for the version in use;
3. this program and its accepted amendments;
4. service or MFE documentation;
5. legacy convention documents.

The existing `docs/conventions/http-api.md` and `api-errors.md` remain a record of
the current migration direction until the new HTTP/error ADR is accepted. They
must then be updated in the same change that activates the new contract.

## Non-goals

- A big-bang rewrite or coordinated release of all services and remotes.
- Moving browser APIs to gRPC.
- A shared domain-model package used by every service.
- Replacing service-owned databases with cross-service joins.
- Returning HTTP 200 for failed operations to preserve a universal envelope.
- Changing business rules merely to make transport code look uniform.
- Trusting browser-provided user, tenant, organization, role, or permission headers.

## Program governance

The program needs four named responsibilities even when one person holds more
than one role:

| Responsibility | Accountable for |
| --- | --- |
| Program owner | Scope, sequencing, risk acceptance, final phase gates |
| Architecture owner | ADRs, boundaries, compatibility and contract decisions |
| Domain owner | Business invariants, endpoint behavior and migration validation |
| Release owner | Environments, canary, rollback, backup/restore and incident decision |

Every work package has one owner. Multiple agents may work in parallel only when
their file scopes and contracts do not overlap, or when a prerequisite contract
has already been merged and pinned.

## Program-level success measures

Exact numeric SLO targets are approved in Phase 0 after baseline capture. The
program still requires the following measurable outcomes:

- 100% of externally reachable routes are cataloged and covered by deny-by-default policy.
- 100% of migrated endpoints pass schema and consumer contract tests.
- 100% of tenant-scoped repository operations include an explicit verified scope.
- 100% of cross-service calls carry trace context and authenticated workload identity.
- 100% of commands classified retryable have an idempotency contract.
- No secrets, session cookies, access tokens, or presigned URLs appear in logs or tracked config.
- Every migration wave has a tested rollback or forward-recovery procedure.
- Performance and error-rate budgets do not regress beyond the approved canary threshold.

## How to use this plan when implementation begins

1. Select only work packages whose prerequisites are complete.
2. Copy the task packet from `08-templates-and-checklists.md`.
3. Give the coding agent exact repositories, files, acceptance tests, and non-goals.
4. Require the agent to inspect dirty worktrees and preserve unrelated changes.
5. Merge foundations before consumers; never let separate agents invent competing contracts.
6. Run the phase gate before starting the next migration wave.
7. Update the endpoint catalog, decision register, and migration ledger in the same PR.

No implementation task should be created from a phase title alone. Use the
work-package IDs and task packet so scope, dependencies, verification, and
rollback remain explicit.
