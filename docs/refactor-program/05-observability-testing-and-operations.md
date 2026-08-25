# Observability, Testing and Operations

## 1. Observability before migration

Telemetry foundations must land before broad domain changes. Otherwise canary
failures cannot be attributed to edge, gateway, service, database, dependency,
frontend bundle, or user behavior.

Use three identifiers with distinct meaning:

- `request_id`: one logical user/API request, preserved through controlled retry;
- `trace_id`: distributed trace containing attempts and downstream spans;
- `event_id`: durable event delivery/deduplication identity.

## 2. Distributed tracing

- Use W3C `traceparent`/`tracestate` across HTTP, gRPC and event boundaries.
- Gateway starts or continues the trace after validating inbound headers.
- FE creates/propagates request correlation according to browser telemetry policy.
- Retry and step-up attempts are child spans, not unrelated requests.
- DB spans record operation/table and duration, never raw sensitive parameter values.
- Messaging spans link producer, outbox publish and consumer processing.
- Sampling retains errors and high-latency traces while controlling cost.
- Trace IDs are returned in safe support-visible errors where available.

## 3. Structured application logs

One JSON log schema should include when applicable:

```text
timestamp, level, service, version, environment
message, code
request_id, trace_id, span_id
route_template or rpc_method
actor_kind and pseudonymous/internal actor_id
tenant_id, org_id
duration_ms, status/outcome
error_class, retry_count
```

Rules:

- use route templates, not resource IDs, as metric/log grouping keys;
- redact authorization, cookies, tokens, OTP, password, DSN credentials and presigned URL query strings;
- do not log complete request/response bodies by default;
- maintain a tested redaction library and sample fixtures;
- application logs are not a substitute for audit records.

## 4. Metrics and SLOs

### API RED metrics

- request rate;
- error rate by status class/stable error code;
- duration p50/p95/p99 by route template;
- in-flight requests and saturation;
- response size.

### Dependency metrics

- DB pool usage/wait, query duration, lock wait, deadlocks and slow queries;
- gRPC method duration/status/deadline/retry;
- NATS publish acknowledgement, consumer lag, redelivery, DLQ;
- Redis latency/error and cache hit rate where meaningful;
- Zeebe job/incident/backlog;
- object storage upload/download/error and abandoned multipart sessions;
- gateway policy deny/missing-policy counts;
- Cloudflare colo/cache/origin timing separately from origin latency.

### Frontend metrics

- shell and remote load success/duration/version;
- route navigation and lazy chunk duration;
- API duration separated from client queue/render time;
- error boundary, unhandled rejection and normalized API problem code;
- Web Vitals by route/device class;
- step-up prompt/success/failure/retry outcome.

### SLO process

Phase 0 records baseline and Phase 2 approves SLOs. At minimum define availability,
latency, correctness and freshness for critical user journeys. Alert on actionable
symptoms and burn rate, not every individual transient error.

## 5. Performance budgets

Budgets are set per endpoint class after baseline. Initial planning boundaries:

```text
public latency = edge/network + gateway/auth + service + DB + downstream + encoding
```

Measure each component. The known public path may reach a non-local Cloudflare
colo, so public TTFB cannot be used as evidence of slow origin without internal timing.

For each migrated slice capture:

- cold and warm public p50/p95/p99;
- direct origin/gateway and service timing;
- DB query count and slowest plan;
- payload compressed/uncompressed size;
- achieved throughput, errors and saturation;
- FE render/request waterfall where applicable.

Load tests report completed requests, dropped iterations, failure rate, p95/p99,
timeouts and resource saturation; configured target RPS alone is not a result.

## 6. Test strategy

### Characterization tests

Freeze important current behavior before changing internals. Mark known bugs as
explicit expected failures or migration cases instead of accidentally preserving them.

### Unit tests

- domain rules and state transitions;
- policy/scope evaluators;
- parsers/mappers/error classifiers;
- query builders and idempotency behavior;
- FE adapters, query keys, reducers and validation mapping.

### Contract tests

- runtime HTTP response validates against OpenAPI;
- generated FE client fixtures validate against the same schemas;
- Proto breaking check and serialization compatibility;
- event schema compatibility and consumer fixture tests;
- stable error-code registry coverage.

### Integration tests

- gateway route -> policy -> verified context -> service;
- repository against real PostgreSQL including tenant isolation and concurrency;
- outbox -> JetStream ack -> inbox consumer/dedupe;
- service-to-service auth, deadline and status mapping;
- object storage multipart/upload/complete and access control;
- OAuth callback, session invalidation, MFA/trusted-device and recent auth.

### End-to-end tests

Prioritize critical journeys rather than every UI branch:

- sign in/session expiry/logout;
- select verified org and navigate remotes;
- list/filter/detail/create/update representative resource;
- high-risk management action against another target user;
- upload/attach/download;
- workflow submit/progress/retry;
- finance command duplicate/concurrency behavior.

### Migration tests

- old code with expanded schema;
- new code with pre-backfill, partial-backfill and completed state;
- restart/resume backfill;
- mixed old/new deployed versions;
- rollback/forward recovery;
- production-scale lock and query-plan rehearsal.

### Security tests

- missing/wrong permission and scope;
- forged privileged headers;
- direct service access;
- unknown route/policy;
- actor/target confusion;
- cross-tenant/cross-org IDs;
- stale session/security stamp/recent auth;
- CSRF/origin/OAuth state replay;
- malformed/oversized inputs and upload constraints;
- log/trace secret redaction.

## 7. CI quality gates

### Backend

- format/vet/static checks selected by the Go tooling ADR;
- test all Go workspace modules, not only the workspace root;
- migration lint and clean-database apply;
- OpenAPI lint/runtime conformance and breaking check;
- Proto lint/generation/breaking check;
- route-policy coverage;
- dependency and secret scanning;
- container build and vulnerability policy.

### Frontend

- frozen lockfile;
- package-boundary and cycle check;
- TypeScript, lint, unit/component tests;
- contract client generation diff check;
- current/previous shell-remote compatibility builds;
- accessibility checks for migrated flows;
- bundle/remote-entry budget;
- browser e2e for selected journeys.

### Infrastructure

- manifest schema/policy validation;
- rendered manifest diff by environment;
- secret-reference check;
- rollout and rollback smoke tests;
- observability dashboard/alert provisioning validation.

No phase can claim completion from compile success alone.

## 8. Environments and test data

- Local: reproducible dependencies or explicit mocks; no dependence on production-like shared credentials.
- Integration: real Postgres, NATS/JetStream, Redis and representative internal auth.
- Staging: production-like routing, policy, TLS/service identity, OTel and data volume sample.
- Production: canary/feature switches and reversible data migrations.

Use seeded synthetic data with multiple tenants/orgs, users with different roles,
cross-scope resource IDs, large lists, duplicate commands and event redelivery.
Never copy production credentials or unredacted production PII into lower environments.

## 9. Secrets and supply chain

Before implementation waves:

- rotate credentials and sessions that were exposed in chat, tracked config, logs or test artifacts;
- replace tracked runtime secrets with placeholders and secret references;
- inventory owners, consumers, rotation interval and emergency revoke path;
- prevent secrets from entering build arguments, images, logs and generated frontend bundles;
- pin dependencies and retain lockfiles/checksums;
- generate SBOM/provenance according to the deployment policy;
- scan repository history and current artifacts, then handle findings through a private security process.

## 10. Release, canary and rollback

- Deploy compatibility foundation before consumer switch.
- Use feature switches at adapters/route selection, not inside every business condition.
- Canary by internal user/tenant or traffic slice only when data semantics remain safe.
- Define automatic halt thresholds for error, latency, auth denial anomaly and business invariant failure.
- Observe application health and user journey success in addition to pod readiness.
- Roll back application versions only when schema remains backward compatible.
- Prefer forward recovery after an irreversible data change.
- Keep previous MFE artifacts and remote manifest available for the compatibility window.

## 11. Operational readiness

Each migrated capability has:

- owner and escalation path;
- dashboard and SLO;
- alerts with runbook links;
- dependency and failure-mode list;
- backup/restore or replay procedure;
- feature switch/canary controls;
- rollback/forward-recovery steps;
- known limitations and compatibility expiry date.

## 12. Observability, quality and operations definition of done

- One request can be followed from browser through gateway, service, DB and events.
- Logs and traces pass redaction tests.
- SLO and canary thresholds are approved and observable.
- Characterization, contract, security and migration tests pass.
- Production-like performance does not exceed the approved regression budget.
- Secret delivery and rotation path is operational.
- Backup/restore or event replay has been rehearsed.
- Runbook and rollback have named owners and evidence.
