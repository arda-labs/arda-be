# Phase 0 ADR Register

Status: accepted-with-follow-up for the landed foundation slices; unresolved
open decisions remain explicit release gates and are not silently treated as
approved production policy.

## ADR-001 — Public HTTP success and error profiles

Status: accepted-with-follow-up. Follow-up: API-01..05 and QA-02 must close
remaining endpoint inventory and provider-owned protocol exceptions.

Proposed decision: migrated JSON resources/results use
`{result, success, errors, messages, meta}`; offset/cursor lists put their typed
list result under `result`; errors use RFC 9457-compatible
`application/problem+json` with stable `code`, `errors[]`, `request_id` and
optional `trace_id`. HTTP status remains authoritative. Binary/SSE/gRPC use
native protocol shapes.

Open decisions: exact `type` URI host, whether `meta` is mandatory on every success,
legacy adapter placement, and compatibility window.

Owner: API architecture + FE platform. Dependency: endpoint/consumer inventory.

## ADR-002 — Versioning and compatibility

Status: accepted-with-follow-up. Follow-up: FE-08 and OPS-03 must provide the
remote compatibility manifest, canary cohort and removal telemetry.

Proposed decision: additive compatible changes stay within a version; breaking
changes use explicit version or time-bounded boundary adapter. MFE shell supports
current and previous remote/client contract during independent deployment.

Open decisions: URL/header versioning, deprecation period, manifest compatibility
mechanism and exact removal telemetry threshold.

Owner: architecture + release. Dependency: FE federation inventory.

## ADR-003 — Actor, target, scope and assurance

Status: accepted-with-follow-up. Follow-up: SEC-04/05/07 must complete the
resource-level policy matrix, freshness behavior and recent-auth evidence.

Proposed decision: authorization evaluates actor, action, target, verified tenant/org
scope, delegation and assurance. `/me` binds target to actor; management routes may
target another ID and audit both.

Landed target-tenant boundary: management requests must carry a verified gateway
actor (`X-Auth-Checked`, actor ID and actor tenant) plus an explicit target
`tenant_id`. Same-tenant management is the default; cross-tenant management is
denied unless the verified actor has the explicit `SUPER_ADMIN` role or
`superadmin` capability. A target user/resource ID is never inferred from the
actor ID. Organization-level policy and any finer-grained delegation remain
follow-up decisions.

Open decisions: resource-level policy engine shape, anti-enumeration status choice,
anti-enumeration status choice and recent-auth methods.

Owner: security + IAM + domain owners.

## ADR-004 — Edge routing and deny-by-default policy

Status: accepted-with-follow-up. Follow-up: SEC-01 and OPS-05 must reconcile the
gateway registry with ingress and rendered environment routes.

Proposed decision: one route/policy registry is required; every public route matches
exactly one policy; unknown/missing policies fail closed. Domain routing remains
explicit; generic proxy is not authorization.

Open decisions: registry source format, startup-fail versus runtime-deny behavior,
Traefik/gateway synchronization mechanism and internal route exposure.

Owner: edge/platform + auth-gateway.

## ADR-005 — OpenAPI and FE client strategy

Status: accepted-with-follow-up. Follow-up: API-01 and QA-02 must expand the
pilot into the supported public surface and add breaking-change comparison.

Proposed decision: OpenAPI is public REST source of truth; specs are linted/breaking-
checked and generate/reuse FE wire types/clients behind domain adapters.

Open decisions: generator, generated artifact location, CI versus committed output,
schema split and runtime conformance tool.

Owner: API architecture + FE platform.

## ADR-006 — Internal service identity

Status: accepted-with-follow-up. Landed choice: mTLS plus a short-lived signed
workload assertion. Follow-up: INT-02/OPS-01 must provision, rotate and prove
the certificate trust roots in each environment.

Proposed decision: internal service identity is independent of user permission
headers and network location. Protected gRPC/HTTP calls authenticate workload,
propagate delegated actor only when explicitly allowed, and authorize method/destination.

Open decisions: mTLS/SPIFFE versus signed internal token or combination, rotation,
local-dev mechanism and migration compatibility.

Owner: platform/security + backend architecture.

## ADR-007 — Events, outbox/inbox and schema evolution

Status: accepted-with-follow-up. Landed choice: JetStream PubAck, stable
`Nats-Msg-Id`, bounded redelivery and explicit DLQ/replay reference. Follow-up:
INT-03/04/05 and QA-05 must prove consumer inbox dedupe and replay.

Proposed decision: domain transaction and outbox commit atomically; publisher waits
for JetStream acknowledgement; consumers ack after local commit and dedupe by event
ID/handler version. Events use CloudEvents-like versioned envelope.

Open decisions: stream naming/partition/order, DLQ operator flow, schema registry,
retention and replay authorization.

Owner: integration + domain owners.

## ADR-008 — Database and idempotency standards

Status: accepted-with-follow-up. Follow-up: DATA-01..04 must finish type/scope
inventory, migration rehearsals and the RLS decision; domain commands must add
request-hash/conflict semantics where required.

Proposed decision: service-owned databases, canonical ID/tenant/time/money types,
explicit scoped repositories, tenant-first uniqueness/indexes, versioned transitions,
and idempotency key scoped by tenant + operation + key.

Open decisions: UUID version, exact decimal scales, soft-delete policy, RLS adoption,
idempotency response retention and global-reference modeling.

Owner: data architecture + service owners.

## ADR-009 — Correlation, tracing, logs and audit

Status: accepted-with-follow-up. Landed choice: request/trace context is
propagated through HTTP, gRPC and event envelopes, with aggregate metrics.
Follow-up: OBS-01..04 must provision export, dashboards, redaction and audit
retention evidence.

Proposed decision: request ID, trace ID, event ID and audit ID remain distinct;
W3C propagation crosses HTTP/gRPC/events; structured logs are redacted; audit is
append-only business/security evidence with separate retention/access.

Open decisions: OTel collector/export, browser trace policy, audit storage/tamper
evidence, PII pseudonymization and retention.

Owner: observability + security + compliance.

## ADR-010 — Migration, canary and recovery

Status: accepted-with-follow-up. Follow-up: OPS-03/04/05 and QA-06 must produce
environment evidence before any legacy route or schema cleanup is marked removed.

Proposed decision: expand-migrate-contract for schema/contracts; feature switches and
canary for consumers; application rollback only while schema is compatible; otherwise
forward recovery. Backup/restore/replay are tested before destructive cleanup.

Open decisions: switch mechanism, cohort selection, automatic halt thresholds,
production approval authority and exact compatibility windows.

Owner: release/platform + data/domain owners.

## Approval gate

Before Phase 2 foundations, each ADR must be marked:

```text
accepted | accepted-with-follow-up | deferred-with-owner-and-date | rejected
```

An `accepted-with-follow-up` decision may unblock only the packages explicitly named
in its consequence section. It cannot be used to hide a missing security/data decision.
