# Phase 0 Service Interaction and Event Topology

Status: initial topology from source/config audit on 2026-08-25.

## Current logical topology

```text
Browser MFE
  -> Cloudflare/Tunnel/Traefik
  -> auth-gateway BFF/policy/session
  -> IAM, Platform, CRM, HRM, Finance, Workflow, Media, Notification

Domain services
  -> gRPC clients where present
  -> internal HTTP clients/legacy calls where present
  -> PostgreSQL per service
  -> Redis where configured
  -> NATS/JetStream for event/outbox paths

Workflow
  -> Zeebe (target: only workflow-service)
```

## Synchronous interaction inventory

| Caller | Callee | Protocol/current pattern | Required contract review |
| --- | --- | --- | --- |
| auth-gateway | IAM | isolated internal HTTP/client adapter | user context, auth version, timeout; private-boundary/service-identity gate |
| auth-gateway | Kratos/Hydra | HTTP/OIDC admin/API | OAuth state, token verification, timeout and secret handling |
| Finance | Platform | gRPC client exists/target | tenant/org metadata, deadline, identity, retry |
| Workflow | CRM/HRM/Finance | mixed/target gRPC | command idempotency, delegated actor, deadline |
| Media | object storage | HTTP/S3 client | timeout, checksum, upload completion, URL secrecy |
| Workflow | notification | gRPC client/server contract | mTLS, signed workload identity, source allowlist, deadline and idempotency |
| domain services | IAM/Platform | direct context/reference lookups | typed contract and cache/freshness behavior |
| browser | auth-gateway | HTTP cookies/session | CSRF/origin, request ID, problem mapping |

This table is intentionally conservative. GOV-04 must validate every client import,
gRPC dial, HTTP base URL and event publisher/consumer against source and deployed config.

## Current internal-call risks

- gRPC clients use deadlines in places, but one uniform fixed timeout is not a complete budget policy.
- Internal gRPC credentials and workload authentication need an explicit accepted mechanism.
- Metadata currently carries several identity fields but needs standardized trace,
  org, delegation and service identity semantics.
- The auth-gateway IAM provider adapter is the remaining internal HTTP
  exception; its private route/service-identity boundary is an explicit gate.
- Retry behavior is not uniformly tied to idempotency.
- Services must not trust internal identity headers without an authenticated boundary.

## Event topology to verify

| Producer/area | Event purpose | Consumer/target | Required controls |
| --- | --- | --- | --- |
| Media | file upload/processing state | Media/domain/notification | transactional outbox, ack, inbox/dedupe |
| Notification | delivery/outbox events | NATS/JetStream consumers | durable ack, retry, DLQ, replay |
| Workflow/domain | case/task/state side effects | notification/audit/projections | CloudEvents-like envelope, ordering, idempotency |
| Platform/reference | future cache invalidation | domain projections/cache | source-of-truth and stale-read policy |
| Audit/security | append-only evidence | audit store/queries | integrity, retention, no secret payload |

## Required interaction row

Every call/topic must ultimately have:

```yaml
caller: service-or-mfe
callee_or_topic: service/method/topic
protocol: http | grpc | nats | s3 | browser
identity: user_session | service | delegated_user | public
tenant_org: verified | none | explicit_resolution
deadline_ms: <budget>
retry: none | safe_read | idempotent_command
idempotency: required | optional | not_applicable
trace: request_id + traceparent + event_id as applicable
failure: status mapping, fallback, compensation or DLQ
owner: domain/team
status: cataloged | verified | migrated
```

The current source-side interaction policy is tracked in
`contracts/interactions/arda-interactions-v1.json` and validated by
`scripts/check-interactions.mjs`. It covers the authenticated IAM HTTP adapter
and current gRPC callers with explicit timeout, retry, identity and context
requirements. Runtime latency, retry counters and destination availability
remain staging/canary evidence gates.

## Topology completion gate

- [ ] Every outbound HTTP/gRPC/S3/NATS client is cataloged.
- [ ] Every client has caller identity, destination owner and deadline.
- [ ] Retry is classified and safe for operation semantics.
- [ ] All internal calls propagate trace/request/tenant/org/delegation context as allowed.
- [ ] Every event has producer, consumer, schema version, ack, dedupe, replay and retention.
- [ ] Zeebe access is limited to Workflow service in the target state.
