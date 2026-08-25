# Architecture and Governance

## 1. Refactor strategy

Use an incremental strangler strategy around existing endpoints and capabilities.
The unit of migration is a vertical slice:

```text
MFE route and feature
  -> shared frontend transport
  -> edge route and auth policy
  -> HTTP contract adapter
  -> domain service/use case
  -> repository and database
  -> internal calls/events
  -> telemetry, tests and rollout controls
```

A slice may keep existing tables or business services when they already satisfy
the target invariants. Refactor behavior and architecture separately whenever a
combined change would make rollback or diagnosis unsafe.

## 2. Target system boundaries

### Browser and MFE

- The shell owns session bootstrap, top-level auth state, navigation, remote
  loading, global providers, and top-level error isolation.
- Remotes own domain UI and feature orchestration, but do not create independent
  auth/session implementations.
- All browser API traffic uses one shared transport abstraction. Domain features
  may wrap generated clients, but must not bypass transport policy with raw fetch.
- Shared UI packages own reusable presentation primitives, not domain behavior.
- Independently deployed shell/remotes require a compatible shared-dependency
  matrix and versioned remote manifest or immutable artifact path.

### Edge and gateway

- Cloudflare and Traefik provide edge/network controls and explicit routing.
- auth-gateway owns browser session, OAuth/OIDC/Kratos integration, CSRF/origin
  checks, policy evaluation, verified request context, step-up, and edge error mapping.
- Generic proxying cannot authorize an uncataloged route. Unknown protected routes
  are denied and produce a stable problem response.
- Domain routing remains explicit. auth-gateway is not an accidental service mesh.

### Domain services

- A service owns its business rules, schema, migrations, domain contracts, and events.
- HTTP and gRPC transports call the same application/use-case layer.
- Repositories never infer actor, tenant, or organization from global state.
- Services do not query another service's database or share domain tables.
- Cross-service references are IDs plus validated snapshots where historical display requires them.

### Shared backend libraries

Shared libraries may own stable technical primitives:

- request and trace context;
- HTTP response/problem encoding and query parsing;
- gRPC metadata, deadlines, status mapping and interceptors;
- event envelope and outbox/inbox utilities;
- database transaction helpers;
- common identifiers, paging and time primitives.

They must not own service-specific DTOs, repositories, permission decisions, or
business state machines. A library change that forces every service to release
simultaneously is an architecture warning.

### Infrastructure

- GitOps manifests describe desired runtime state; application repos describe contracts and code.
- Secrets come from a secret-management path, not tracked runtime config.
- Platform observability is available before large migration waves begin.
- Rollout status distinguishes pod readiness, application health, contract health,
  and end-user success.

## 3. Domain ownership rules

Before implementation, every resource and use case must have one owning domain.
Ownership includes:

- canonical ID and lifecycle;
- validation and business invariants;
- write API and database tables;
- domain event names and payloads;
- retention and deletion policy;
- compatibility and deprecation decisions.

Read models may be copied into another service through events. Copies are
projections, not alternate sources of truth.

Candidate ownership to confirm in Phase 0:

| Area | Proposed owner |
| --- | --- |
| Identity mapping, users, roles, permissions, sessions, MFA | IAM |
| Browser login/session bridge and route policy enforcement | auth-gateway |
| Organizations and shared reference data | Platform |
| Customers and customer workbench | CRM |
| Employees, positions and HR organization units | HRM |
| Ledger, accounting transactions and approvals | Finance |
| Cases, process definitions, task/SLA facade and Zeebe access | Workflow |
| File metadata, upload sessions and object access | Media |
| Delivery preferences, templates and notification delivery | Notification |

## 4. Required decision records

Phase 0 must approve the following ADRs before foundational implementation:

| ADR | Decision |
| --- | --- |
| ADR-001 | Public HTTP success profiles and RFC 9457 error representation |
| ADR-002 | API versioning, compatibility window and deprecation policy |
| ADR-003 | Actor, target, tenant, org, delegation and recent-auth model |
| ADR-004 | Edge routing ownership and deny-by-default route registry |
| ADR-005 | OpenAPI ownership, generation and FE client strategy |
| ADR-006 | gRPC service identity and authorization mechanism |
| ADR-007 | Event envelope, JetStream delivery, outbox/inbox and schema evolution |
| ADR-008 | Database ID, tenant, money, timestamp, soft-delete and versioning standards |
| ADR-009 | Trace/request correlation, log schema and audit separation |
| ADR-010 | Feature flag, canary, rollback and database migration strategy |

Each ADR records context, decision, alternatives, consequences, migration impact,
security impact, and rollback/exit strategy. Decisions must not be embedded only
in implementation PR descriptions.

## 5. Compatibility policy

### Public HTTP

- Breaking changes require a new version or an explicitly time-bounded compatibility adapter.
- Adding optional fields is compatible; changing meaning, type, requiredness, or enum behavior is not.
- Deprecation includes owner, announcement date, final supported release, telemetry,
  and removal criterion.
- Old and new representations may coexist at the boundary, but domain logic has
  one canonical internal model.

### MFE releases

- The shell must support the currently deployed and immediately previous compatible
  remote contract during independent rollout.
- Shared singleton dependency versions are checked before deployment.
- A remote must fail in its error boundary when incompatible, not corrupt the shell.
- Contract changes merge before consumer migration and remain backward compatible
  until all deployed consumers have moved.

### Events

- Event names and semantic meaning are immutable.
- New optional fields are preferred over mutating existing meaning.
- Consumers ignore unknown fields.
- A breaking semantic change creates a new event type/version and a transition plan.

## 6. Current-state artifacts required in Phase 0

Create machine-readable catalogs rather than prose-only inventories:

1. Endpoint catalog: route, method, owner, consumer, policy, scope, risk, response profile.
2. FE consumer catalog: feature, API call, expected shape, query key, auth behavior.
3. Data catalog: table, owner, scope, ID type, unique constraints, retention, PII class.
4. Interaction catalog: caller, callee/topic, protocol, deadline, retry, auth, idempotency.
5. Event catalog: producer, event type/version, consumers, ordering, dedupe and retention.
6. Dependency matrix: shell/remotes/packages, service/libs/protos and deploy compatibility.
7. Migration ledger: endpoint/table/event, old state, target state, wave and status.

The catalogs are version-controlled and reviewed by the relevant domain owner.

## 7. Change control

During the refactor:

- New endpoints must use the target contract after ADR approval.
- Existing endpoints are migrated only through a registered work package.
- A temporary adapter has an owner and removal phase.
- No new plain-string HTTP errors or uncataloged proxy routes are accepted.
- No new browser raw-fetch path is accepted outside an approved binary/stream adapter.
- No new cross-service direct database dependency is accepted.
- No schema migration is merged without forward recovery and validation steps.

## 8. Program risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Big-bang release | Vertical slices, compatibility adapters and canary |
| FE and BE contracts drift | OpenAPI generation plus consumer contract tests |
| Shared library becomes a bottleneck | Share technical primitives only; version and test libraries independently |
| Tenant data exposure | Mandatory scope objects, query tests, constraints and optional RLS defense-in-depth |
| Auth regression | Route-policy coverage gate and actor/target authorization matrix tests |
| Duplicate financial/workflow commands | Scoped idempotency record and retry policy |
| Event loss or duplication | Transactional outbox, acknowledged publish, inbox/dedupe and replay tests |
| Migration lock/outage | Expand-migrate-contract, batch backfill and observed lock budget |
| Independent MFE incompatibility | Manifest/version compatibility checks and previous-version test matrix |
| Undiagnosable production failure | Telemetry foundation before domain migration waves |

## 9. Definition of architecture-ready

The program may leave Phase 0 only when:

- all required ADRs have an accountable approver and status;
- endpoint, consumer, data and interaction catalogs have owners and initial coverage;
- the first three pilot slices are selected;
- baseline correctness, latency, query and error-rate measurements are stored;
- dirty/legacy behavior that must remain temporarily is identified explicitly;
- rollback authority and incident decision path are named;
- work packages for Phases 1-3 have non-overlapping scopes and dependencies.
