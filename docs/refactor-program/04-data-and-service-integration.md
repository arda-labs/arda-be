# Data and Service Integration

## 1. Data ownership

- Each service owns its database/schema and migrations.
- Other services use API, gRPC, or event contracts; they do not query owned tables.
- Cross-service database foreign keys and distributed SQL joins are prohibited.
- A copied read model is explicitly labeled as a projection with source, version,
  refresh/replay path, and acceptable staleness.
- Domain owners approve invariants and migration reconciliation, not only DB maintainers.

## 2. Canonical database standards

ADR-008 must select exact types. The proposed baseline is:

| Concern | Target rule |
| --- | --- |
| Primary IDs | One sortable UUID strategy across new tables; UUID DB type, not mixed text/varchar |
| Tenant/org IDs | Same canonical type as owning resource; no magic `default` value for scoped rows |
| Money | `NUMERIC(p,s)` with domain-specific scale; decimal string at JSON boundary |
| Time | `timestamptz`, stored in UTC; business date is a separate `date` field |
| Status | constrained value or state table; unknown values rejected |
| Boolean | non-null when a real binary state; nullable only for meaningful unknown |
| JSON | used for extension/snapshot data, not to avoid relational invariants |
| Version | integer/version token for concurrent mutable resources |
| Audit columns | `created_at`, `created_by`, `updated_at`, `updated_by` where meaningful |
| Soft delete | only with documented restore/uniqueness/query semantics |

Table and column naming remain `snake_case`. Foreign keys are used within a
service boundary where lifecycle permits them.

## 3. Tenant and organization invariants

For tenant-scoped tables:

- tenant ID is non-null;
- unique constraints begin with tenant ID unless global uniqueness is intentional;
- indexes support tenant-first query predicates;
- child references enforce or validate the same tenant/org relationship;
- repository lookup/update/delete includes verified scope;
- background jobs process an explicit tenant partition;
- export/report paths cannot omit tenant filters accidentally.

RLS may be introduced as defense-in-depth after connection and transaction
semantics are proven. It does not replace explicit application scoping. If used:

- application connects as a non-owner role;
- scope is set transaction-locally;
- pool reuse cannot leak scope;
- migrations/admin jobs use separate privileged roles;
- tests prove cross-tenant access fails at both application and DB layers.

## 4. Repository contract

Repository methods accept an explicit scope and operation context:

```go
type Scope struct {
    TenantID uuid.UUID
    OrgID    *uuid.UUID
}

GetTransaction(ctx context.Context, scope Scope, id uuid.UUID)
UpdateTransactionStatus(ctx context.Context, scope Scope, id uuid.UUID, expectedVersion int, next Status)
```

Rules:

- no implicit global tenant;
- no tenant-scoped `GetByID(id)`/`DeleteByID(id)` without scope;
- updates/deletes check `RowsAffected` and distinguish not-found, stale version and forbidden scope safely;
- dynamic sort/filter columns are allowlisted, never interpolated from raw input;
- list queries have deterministic ordering and unique tiebreaker;
- `%keyword%` search is replaced with appropriate trigram/full-text indexes when justified;
- transactions live at the application/use-case boundary, not hidden across unrelated repository calls;
- state transitions use optimistic version or row locking according to contention/invariant.

## 5. Domain state transitions

For each aggregate/state machine, document:

- allowed current -> next states;
- actor/action required;
- preconditions and separation-of-duties constraints;
- database lock/version behavior;
- emitted event/outbox record;
- idempotent repeated-command outcome;
- compensation or recovery after downstream failure.

Finance posting/reversal, approval, workflow case/task transitions, media completion,
and identity security changes require explicit state-transition tests.

## 6. Idempotency

Idempotency is scoped to a business operation, not a globally unique text column.

Proposed record:

```text
tenant_id
operation
idempotency_key
request_hash
status: processing | completed | failed
resource_id
response_status
response_snapshot or result reference
created_at, expires_at
```

Unique key: `(tenant_id, operation, idempotency_key)`.

Behavior:

- same key and same request returns/references the original result;
- same key and different request hash returns conflict;
- concurrent duplicates serialize safely;
- processing timeout has an explicit recovery rule;
- retention matches client retry and business duplicate risk;
- downstream idempotency keys derive from the parent operation without colliding across operations.

## 7. Database migration method

Use expand-migrate-contract:

1. **Expand**: add compatible schema/index/nullable field or parallel representation.
2. **Dual-compatible code**: new code handles old and new state without changing behavior.
3. **Backfill**: bounded batches with checkpoint, rate limit, retry and observability.
4. **Validate**: counts, checksums, constraints, sampled domain reconciliation and tenant isolation.
5. **Switch**: feature flag/read preference to the new representation.
6. **Observe**: hold for the approved compatibility period.
7. **Contract**: remove old write/read path and only later remove schema.

Migration requirements:

- preflight on production-like data volume;
- measured lock duration and query plan;
- backup and restore rehearsal;
- restart-safe backfill;
- no irreversible destructive step in the same release as the switch;
- forward recovery for migrations that cannot be rolled back safely;
- migration ledger with status per environment.

## 8. Immediate data risks to include in the catalog

The current-state audit identified these high-priority patterns to verify and
remediate through dedicated work packages:

- finance idempotency keys with global rather than tenant/operation scope;
- finance reads/updates/approvals that do not consistently filter tenant;
- ledger/account references that may not enforce same-tenant ownership;
- media get/delete access that needs tenant/org/visibility enforcement;
- workflow case get/update operations without consistent tenant predicate;
- mixed UUID and text/varchar identifiers and tenant types;
- tracked/default database credentials and `default` tenant assumptions.

These are planning findings until refreshed against source at implementation time.

## 9. Synchronous service integration

Use gRPC for immediate typed internal query/command calls where the caller needs a
result to continue. Keep HTTP for browser/external boundaries and operational endpoints.

Every gRPC client must define:

- destination and method allowlist;
- workload authentication;
- request/delegated actor context;
- `traceparent` and request ID propagation;
- deadline based on caller budget;
- retry classification;
- max request/response size;
- status/detail mapping;
- metrics by logical method, not high-cardinality resource ID.

Every server uses interceptors for authentication, validated context, tracing,
metrics, panic recovery and safe status mapping. Business logic remains below the
transport layer.

### Deadline budgeting

One fixed five-second timeout for every internal call is not a policy. Each public
request has a total budget; downstream calls receive a smaller propagated deadline
that leaves time for mapping and response. Cancellation propagates to DB queries
and child calls.

### Retry policy

- Retry only transient statuses and only when operation safety is known.
- Reads may be retried with bounded exponential backoff/jitter inside deadline.
- Commands require idempotency before retry.
- Never layer uncontrolled retries at browser, gateway, service and client simultaneously.
- Circuit breaking/load shedding is added only with measured thresholds and fallback semantics.

## 10. Asynchronous integration

Use events for notifications, audit fan-out, search/read projections, media
processing and domain state propagation where immediate consistency is unnecessary.

### Event envelope

Adopt a CloudEvents-like envelope:

```json
{
  "specversion": "1.0",
  "id": "...",
  "source": "arda.finance",
  "type": "vn.arda.finance.transaction.posted.v1",
  "subject": "transactions/...",
  "time": "2026-08-25T00:00:00Z",
  "datacontenttype": "application/json",
  "tenant_id": "...",
  "org_id": "...",
  "request_id": "...",
  "traceparent": "...",
  "actor": { "kind": "user", "id": "..." },
  "data": {}
}
```

The event contains the minimum durable business fact. Do not publish tokens,
permission lists, mutable presigned URLs, or unnecessary PII.

### Outbox and publisher

- Domain state and outbox row commit in one DB transaction.
- Publisher claims rows safely, publishes to JetStream, waits for acknowledgement,
  and only then marks published.
- Retry count, next attempt, last error, published sequence and timestamps are observable.
- Poison records move to an explicit dead-letter/review path without blocking the partition.

The notification reference implementation uses JetStream manual acknowledgement,
bounded redelivery and a stream-local `arda.dlq.*` subject after the maximum
delivery count. Its database outbox also stores dead-letter state and permits
one-event replay only with an operator identity; a real integration run must
still prove inbox dedupe and replay reconciliation.

### Inbox and consumers

- Consumer is durable and explicitly acknowledges after local commit.
- Inbox/dedupe records event ID and handler/version.
- Handler is idempotent and safe under redelivery.
- Ordering assumptions are per aggregate/subject and documented.
- Replay procedure, retention and schema compatibility are tested.

## 11. Workflow boundary

- Only Workflow service connects to Zeebe.
- Domain services submit/query workflow through typed Workflow gRPC contracts.
- Workflow workers call domain commands through authenticated typed clients.
- Domain truth remains in the domain service; Workflow stores orchestration state and projections.
- Idempotency key passes intact from public command through workflow submission and callbacks.
- Failures have visible incident/retry/compensation states, not silent log-only recovery.

## 12. Media boundary

- Media owns file metadata, upload session, storage key, checksums, visibility and processing state.
- Calling domain owns the attachment/reference and validates whether the actor may attach it.
- Get/delete/download authorize tenant/org/owner/visibility for the requested media resource.
- Short-lived URLs are generated on demand and never persisted as durable profile/document data.
- Upload completion is idempotent and verifies object existence, size, checksum/type constraints.
- Outbox publication and async processing state are observable and replayable.

## 13. Data and integration definition of done

- Data owner and scope are documented.
- Schema types and constraints satisfy the accepted standards.
- All repository operations use explicit scope and pass cross-tenant negative tests.
- State transitions are atomic and concurrency-tested.
- Commands are idempotent or explicitly non-retryable.
- Internal calls are authenticated, deadline-bound and traced.
- Events use accepted schema, outbox acknowledgement and inbox dedupe.
- Migration has production-like rehearsal, validation and recovery evidence.
- No cross-service DB access or shared mutable domain DTO is introduced.
