# Templates and Checklists

## 1. Implementation task packet

Copy this block when assigning a coding agent:

```md
# <WORK-PACKAGE-ID>: <title>

Status: ready
Owner:
Repositories:
Target branch/commit:
Phase:
Dependencies already merged:
Related ADRs/contracts:

## Outcome
One externally verifiable result.

## In scope
- Exact operations/resources.
- Exact packages/directories expected to change.

## Out of scope
- Explicit nearby work the agent must not absorb.

## Current behavior
- Characterization evidence and known defects.

## Target behavior
- Request/response/event/state/security behavior.

## Security and data invariants
- Actor, target, scope, permission, risk/recent auth.
- Tenant/data ownership, idempotency and concurrency.
- PII/secrets/audit rules.

## Compatibility and rollout
- Old consumers/versions supported.
- Feature switch/canary sequence.
- Rollback or forward-recovery.
- Temporary adapter and cleanup package.

## Acceptance criteria
- Observable behavior in Given/When/Then or exact assertions.

## Required verification
- Unit:
- Contract:
- Integration:
- E2E:
- Migration/performance/security as applicable:

## Required documentation updates
- Endpoint/consumer/data/interaction catalog.
- Migration ledger/error registry/runbook.

## Handoff
- Diff summary, commands/results, risks, unrun checks and follow-up IDs.
```

## 2. Endpoint catalog row

```yaml
operation_id: iamAdminListUsers
method: GET
path: /api/admin/users
owner: iam
upstream: iam-service
visibility: public
kind: query-list-offset
consumers:
  - mfe:iam/users
actor: session_user
target: user_collection
scope:
  tenant: required
  org: optional_verified_filter
action: iam.user.read
risk: medium
recent_auth_seconds: null
idempotency: not_applicable
request_schema: '#/components/...'
success_profile: offset_list
errors:
  - auth.unauthorized
  - auth.forbidden
  - validation.invalid_input
rate_limit_class: admin_read
audit: access_log_only
legacy_shapes: []
migration_wave: DOM-IAM
status: cataloged
```

## 3. Management command authorization matrix

| Case | Actor | Target | Scope | Assurance | Expected |
| --- | --- | --- | --- | --- | --- |
| Self with management permission | A | A | same tenant/org | fresh | Explicit policy result |
| Manager changes another user | A | B | permitted relationship | fresh | Allow + audit |
| Missing permission | A | B | valid | fresh | 403 stable code |
| Wrong tenant | A | B | different tenant | fresh | 404 or 403 per anti-enumeration ADR |
| Wrong org | A | B | outside allowed org | fresh | Deny |
| Stale recent auth | A | B | valid | stale | 403 recent-auth problem |
| Forged target/actor header | A | B/C | any | fresh | Header ignored/deny |
| Suspended target exception | A | B | valid | fresh | Domain-specific result |
| Service automation | service | B | delegated scope | workload auth | Explicit service policy |

Each management operation extends this table with target-state and separation-of-duty cases.

## 4. ADR template

```md
# ADR-NNN: <decision>

Status: proposed | accepted | superseded | rejected
Date:
Owners:
Decision deadline:

## Context
Facts, constraints and current failure modes.

## Decision
Exact rule and scope.

## Alternatives considered
Options and why not selected.

## Consequences
Positive, negative, operational and organizational effects.

## Security/data/privacy impact

## Compatibility and migration
Old/new coexistence, sequencing and removal criteria.

## Verification
How CI/runtime proves compliance.

## Reversal/supersession
How to back out or replace the decision.
```

## 5. Database migration runbook

```md
# Migration <id>: <name>

Owner:
Affected service/tables:
Data classification:
Expected rows/size:
Backward-compatible application versions:

## Preconditions
- Backup identifier and restore validation.
- Disk/headroom and replica/lock health.
- Production-like execution timing.

## Expand
DDL, lock expectations and cancellation threshold.

## Backfill
Batch key, batch size, throttle, checkpoint, retry and restart behavior.

## Validation
Counts, checksums, constraints, domain reconciliation and tenant isolation.

## Switch
Feature/read/write switch and canary group.

## Observe
Metrics, duration and abort thresholds.

## Contract
Old code/schema removal prerequisites and earliest date.

## Recovery
Rollback where safe; otherwise forward-recovery steps.
```

## 6. Event contract checklist

- Stable event `type`, source and semantic version.
- Globally unique event ID and aggregate subject.
- Tenant/org and trace context where appropriate.
- Minimal durable business fact; no secrets or unnecessary PII.
- Producer transaction includes outbox insert.
- Publisher waits for JetStream acknowledgement.
- Durable consumer, explicit ack and inbox/dedupe.
- Ordering/partition assumption documented.
- Retry, poison/DLQ and operator action documented.
- Schema compatibility and replay fixtures tested.
- Retention and projection rebuild procedure defined.

## 7. Frontend feature checklist

- Operation exists in accepted OpenAPI.
- Uses generated contract/domain adapter and shared transport.
- No raw fetch for JSON.
- AbortSignal propagates.
- Deterministic query key and exact invalidation.
- URL/list state follows the shared list definition.
- Loading, empty, partial, error and retry states are intentional.
- 401, 403, recent-auth, validation, conflict, 429 and 5xx behavior tested as applicable.
- Actor/target is visible for management actions.
- Stable error codes map to i18n; raw backend messages are fallback only.
- Accessibility and keyboard/focus behavior tested.
- Shell/remote current-previous compatibility passes.
- Bundle and route-load budgets pass.

## 8. Cross-stack definition of done

An endpoint/capability is migrated only when all applicable items pass.

### Contract

- OpenAPI/Proto/event schema approved and generated without drift.
- Success/error profile and stable codes documented.
- Compatibility window and legacy adapter owner recorded.

### Security

- Route policy registered and deny-by-default coverage passes.
- Actor, target, scope, action and assurance tested.
- Internal caller identity authenticated.
- Audit and redaction requirements satisfied.

### Backend/data

- Application layer owns business transaction.
- Repository scope and cross-tenant negative tests pass.
- Constraints/index/query plan are appropriate.
- Idempotency/concurrency behavior is defined and tested.
- Internal calls/events use accepted deadline/delivery contracts.

### Frontend

- Shared transport and contract adapter used.
- Query/cache/form/error/auth behavior passes.
- Accessibility, i18n and federation compatibility pass.

### Quality/operations

- Characterization, unit, contract, integration and selected E2E tests pass.
- Request/trace/event can be correlated.
- Dashboard, SLO/canary and alert/runbook exist as required.
- Performance stays inside approved budget.
- Rollback/forward-recovery is tested.
- Catalogs and migration ledger are updated.

## 9. Phase gate report

```md
# Phase N gate

Date:
Decision: pass | conditional | fail
Approvers:

## Evidence
- Deliverable links and test/dashboard results.

## Open risks
| Risk | Severity | Owner | Compensating control | Expiry |

## Waivers
Explicit accepted deviations only.

## Rollback/recovery readiness

## Next phase packages authorized
List exact IDs, not broad phase names.
```

## 10. Pre-assignment checklist for subagents

- Verify target branch and current Git status.
- Read repository `AGENTS.md` and relevant accepted ADR/contract completely.
- Confirm prerequisite packages are merged, not merely in progress.
- Confirm exact file ownership does not overlap another active package.
- Record existing unrelated changes that must be preserved.
- Re-run focused baseline test before editing.
- Stop if implementation requires changing a decision or expanding scope; return
  an ADR/task dependency instead of silently inventing architecture.
