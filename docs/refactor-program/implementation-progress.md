# Implementation progress

Snapshot: 2026-08-25

This is the execution companion to the plan. It records only changes that have
landed in code and their verification; it does not replace the phase gates.

## Landed foundations

- The internal interaction policy is now a versioned machine-readable contract
  at `contracts/interactions/arda-interactions-v1.json`, covering the current
  authenticated IAM HTTP adapter and gRPC calls with explicit deadline, retry,
  identity and context requirements. `scripts/check-interactions.mjs` runs in
  backend CI; live latency/retry/availability evidence remains an environment
  gate.
- Proto sources are canonical under `proto/arda/**/v1`; media and notification
  contracts are included alongside the original five domains. The generator
  and `scripts/check-proto.mjs` now compare all generated Go files for drift.
- IAM Casbin ABAC now fails closed when the active status/subject context or
  policy enforcer is missing; negative tests cover both cases.
- The unimplemented duplicate IAM public-auth orchestrator/provider surface was
  removed. Browser OAuth, Hydra consent, Kratos flows and session cookies now
  have one owner: auth-gateway; IAM exposes only its user/admin/internal domain
  contracts. Unused Hydra/provider/rate-limit stubs and IAM-only config/env
  references were removed from the service and deployment manifests.
- `scripts/check-security-invariants.mjs` protects the IAM/gateway auth boundary
  from reintroducing implicit policy allows or the removed generic gateway
  proxy fallback.
- Removed the unused IAM `TenantEnforcer` no-op that previously forwarded
  requests without comparing scope; the active `WithTenant` middleware remains
  explicit and tested for missing/unverified tenant context.
- IAM now refuses startup when the Casbin model or policy enforcer cannot be
  loaded; the previous “policy disabled” startup mode is removed so deployment
  readiness cannot hide an authorization outage.
- Workflow’s Notification gRPC client now establishes one persistent mTLS /
  signed-identity connection with request/trace metadata instead of performing
  a new handshake per notification; startup fails if that internal dependency
  cannot be configured.
- IAM and Platform now inject one persistent Media gRPC client per process,
  propagate verified request metadata per call and fail startup when the media
  boundary is not configured; Compose parity now declares `MEDIA_GRPC_ADDR`.
- Notification has a controlled `cmd/notification-replay` operator tool for
  exactly one DLQ event per invocation. It requires an explicit confirmation,
  operator identity and `DATABASE_DSN` environment input, and records the
  replay operator in the DLQ row without exposing payload data. Runtime replay
  and projection-rebuild evidence are still open.
- OAuth callback state is now consumed and validated before handling provider
  errors, and Hydra consent acceptance fails closed on lookup/network/decode
  failures instead of accepting an empty scope. OAuth upstream calls also use
  the inbound request context for cancellation.
- IAM audit persistence now appends synchronously through a transaction-scoped
  PostgreSQL advisory lock, uses canonical UTC timestamps for hash input, and
  removes the legacy non-chained writer. Verification now starts from the
  predecessor anchor before the requested time window. This prevents concurrent
  tail races, monotonic-clock hash drift, request-context loss and false
  tamper reports for bounded audit queries.
- Kratos admin adapters now propagate the caller context through create/get/
  find/update/delete operations and surface request-encoding failures instead
  of discarding them.
- MFA trusted-device lookup and auth-gateway session creation now fail closed
  on device-store/trust errors; a session is not reported as created when its
  device state could not be persisted.
- BFF session-user resolution and session tracking now fail closed when IAM is
  unavailable, and user-context lookup preserves the inbound request context
  through its bounded timeout. A static security gate and regression test cover
  the removed fallback.
- `phase-0/rls-pilot.md` and `scripts/check-rls-pilot.mjs` now define a safe,
  disposable PostgreSQL RLS feasibility pilot; production tables remain
  unchanged until transaction-local tenant context and integration evidence
  exist.
- `contracts/observability/arda-observability-v1.json` and its CI checker now
  define the correlation headers, dependency-neutral HTTP metrics and proposed
  SLO classes. Exporter, dashboards, alerting and approved targets remain
  runtime/operations gates.
- Media is now the reference transport/application/repository layering slice:
  the handler depends on domain/service only, the service owns repository and
  storage orchestration, and `scripts/check-layering.mjs` prevents a direct
  handler-to-repository regression. Other domains remain migration slices.
- Redis-backed OAuth state consumption now uses atomic `GETDEL`; concurrent
  callbacks cannot both redeem the same state. The in-memory store already
  consumed under its mutex, and the auth security invariant checks the Redis
  implementation for a non-atomic GET/DEL regression.

- Auth gateway denies unknown/missing policy routes and strips/rebuilds trusted
  identity headers. Active organization must be a verified session membership.
- Gateway policy loading rejects malformed methods, paths, risk/public mismatches,
  duplicate methods and public routes with permissions.
- FE auth/bootstrap, logout and recent-auth retry use the shared API client. One
  logical request keeps one request ID across the step-up retry.
- The shared client preserves browser session cookies with `credentials:
  "include"`; `/api/auth/me` now has a transport regression test, while BFF
  handler tests verify the `arda_sid` cookie is read. Production GitOps sets
  `COOKIE_SECURE=true` and credentialed CORS remains origin allowlisted.
- The shared FE transport accepts only the canonical `application/problem+json`
  error profile; legacy error envelopes are rejected instead of being parsed as
  a compatibility fallback. Kratos/OAuth raw fetch remains isolated to its
  protocol-owned adapter.
- FE step-up status/verification, OAuth callback/consent and post-login `/me`
  loading now use the shared transport; Kratos flow endpoints remain explicitly
  protocol-native adapters because their response schema is owned by Kratos.
- The browser session read `/api/auth/me` now emits the canonical success
  envelope and both auth bootstrap consumers unwrap `result` explicitly. The
  ForwardAuth 401/403 responses also use the canonical problem profile; the
  remaining redirect and Kratos/OAuth provider payloads stay protocol-owned.
- Shared Go HTTP helpers provide the canonical `{result, success, errors, messages,
  meta}` envelope and RFC-style problem responses. The IAM permissions and media
  pilots now switch directly to the canonical contract; no query-flag fallback is
  retained inside those migrated surfaces.
- Shared gRPC metadata carries request/trace, tenant, active/all organizations,
  actor/target, roles/permissions, assurance and source service. CRM, HRM,
  Workflow and Finance HTTP routers bind BFF context to downstream gRPC calls.
- CRM HTTP mutations, relationship reads and workflow gRPC commands require
  verified tenant/org scope; repository reads/updates/relationship operations
  include tenant/org predicates for the migrated operations.
- CRM customer and amendment JSON endpoints now emit the canonical success
  envelope and `application/problem+json` directly; the CRM MFE adapter unwraps
  typed `result` values while workflow/platform consumers remain separate
  contracts.
- Media upload/attach/retrieve/delete now require tenant + active organization;
  scoped repository queries prevent cross-tenant/org access by public ID.
- IAM permissions list is the first direct canonical list migration. Its gateway
  policy uses `iam.permission.read/create/delete` instead of the broad user
  permission, and the MFE adapter consumes the typed `result` list.
- HRM reference-data and employee-registration endpoints now emit canonical
  success/problem envelopes; the HRM MFE adapter unwraps typed list and mutation
  results without shape detection.
- Finance account, ledger, operation, approval and accounting-config handlers
  now use canonical success/problem helpers. The Finance MFE adapter consumes
  typed `result` values, and its operation screen no longer falls back to local
  mock data when an endpoint is unavailable.
- Workflow JSON handlers and the workflow/workbench FE transports now use the
  canonical envelope; XML and SSE remain native content profiles.
- Notification JSON endpoints and Platform JSON handlers now use canonical
  success/problem responses; the notification and platform FE adapters unwrap
  the typed result centrally.
- Finance handlers fail closed when BFF tenant or authenticated actor context is
  absent; HRM workflow-case creation no longer invents a tenant or actor.
- IAM user/group/role creation requires an explicit target `tenant_id`; role
  reads now preserve `tenant_id` instead of silently dropping it. This keeps
  management target scope distinct from authenticated actor identity.
- IAM management list and target operations now require a verified actor,
  explicit target tenant and same-tenant scope; cross-tenant management is
  denied unless the verified context carries the explicit `SUPER_ADMIN` role or
  `superadmin` capability. The MFE users/groups/roles and picker queries send
  the target tenant instead of relying on an omitted query or a synthetic
  `default` value. Unit/typecheck evidence covers the new fail-closed matrix.
- IAM repositories no longer widen role or group mappings to the legacy
  `default` tenant. A new migration drops synthetic IAM tenant defaults and
  rejects empty/`default` values for new users, roles and groups without
  reassigning historical rows; the bootstrap super-admin tenant is passed
  explicitly as a reserved system value.
- IAM admin user/group/role/permission/audit endpoints now emit the canonical
  success/problem profiles directly; the MFE admin adapter unwraps `result`
  through one typed transport helper. Tenant management forms no longer submit
  an invented `default` tenant.
- IAM self-service profile, session/device and MFA JSON endpoints now use the
  same canonical profiles; auth-gateway's IAM client and account/media/step-up
  consumers are migrated in lockstep. Internal IAM service endpoints remain a
  separately owned internal profile.
- Migrated service routers return canonical method-not-allowed problems instead
  of ad-hoc JSON errors; the gateway also returns canonical origin/method
  problems. Protocol-native health, OAuth/Kratos, XML and SSE profiles remain
  explicit exceptions.
- IAM policy management/enforcement now uses the canonical profile and fails
  closed when the policy enforcer is unavailable instead of dereferencing a
  disabled implementation.
- Auth-gateway's IAM HTTP calls remain a deliberately isolated provider/auth
  lifecycle adapter rather than a generic domain client; they are not included
  in browser response-shape migration. This remains a security/runtime gate
  until the internal route boundary is covered by private network policy, or
  replaced by a typed IAM auth gRPC contract. The current adapter now carries
  the same short-lived signed workload assertion as gRPC and IAM rejects
  missing, expired or non-`auth-gateway` callers on `/internal/iam/*`.
- Finance idempotency is scoped by `(tenant_id, operation_name, idempotency_key)`;
  the global key uniqueness was removed by migration, lookup/create race handling
  re-reads only within that scope, and service layers reject missing tenant or
  authenticated actor instead of inventing defaults.
- Finance account/balance/transaction/approval lookup and mutation paths now
  require tenant-scoped repository predicates and explicit BFF tenant context;
  UUID-only access is no longer accepted by the migrated handlers.
- Platform tenant-owned resource creation no longer substitutes the synthetic
  `default` tenant; organization, credit institution, area and file-template
  repositories reject missing tenant scope, and identifier reads/mutations use
  tenant predicates with the verified `X-Tenant-Id` context.
- HRM reference data and employee registrations now carry a required
  `tenant_id`; repository reads, writes and deletes use the verified outgoing
  metadata scope, and the HTTP router rejects missing/unverified tenant context.
  The migration intentionally stops on non-empty unassigned tables instead of
  fabricating a tenant, and replaces global code uniqueness with tenant-first
  uniqueness.
- Platform FE list adapters no longer send a caller-selected `tenant_id` query
  parameter; the backend derives scope from the verified request context while
  management target tenant fields remain explicit in IAM forms/contracts.
- Workflow HTTP routes now reject missing/unverified tenant context. Case list,
  read, create, submit, claim, process-key and timeline access use tenant
  predicates; worker projection timeline writes are isolated behind an explicit
  internal method rather than weakening the public repository boundary.
- The unused CRM `/api/v1/customers` and Workflow `/api/v1/workflows/*` aliases
  were removed; current FE consumer search contains no supported caller for
  those paths, and the canonical `/api/crm/*` and `/api/workflow/*` routes remain.
- Workflow Process Roles no longer converts failed API calls into empty/mock
  state. It displays the real error and exposes an explicit retry action.
- Internal gRPC clients and servers now require mTLS transport plus a short-lived
  signed workload assertion, verify the destination audience and allowlisted
  source service, and fail startup/dial when `ARDA_SERVICE_AUTH_SECRET` or the
  `ARDA_GRPC_{CA,CERT,KEY}_FILE` material is absent. Runtime certificate
  provisioning, rotation, and evidence remain deployment gates; plaintext is
  not an accepted fallback. Media attachment and Workflow -> Notification
  acceptance are now typed gRPC-only internal commands; both verify the
  asserted source service at the destination.
- Notification outbox publishing now uses a durable JetStream stream and waits
  for PubAck; the outbox row ID is sent as `Nats-Msg-Id` for retry deduplication.
  Core NATS fire-and-forget publishing is no longer used. Consumer ack/replay/
  integration evidence remains a separate gate; a bounded retry policy and
  operator-identity-gated one-event DLQ replay path are now represented in the
  notification schema/repository.
- The notification event package now includes a durable manual-ack consumer
  reference with bounded redelivery and `arda.dlq.*` publication after the
  delivery limit; it is intentionally not enabled for a business handler until
  an inbox/dedupe table and integration fixture are assigned. The generic
  `noti_event_inbox` migration and `ProcessEventOnce` transaction primitive now
  provide that boundary for the future handler, while no business handler is
  enabled without its event decoder and fixture. DLQ publishes now carry a
  stable JetStream message ID so an interrupted post-publish ack cannot create
  duplicate dead-letter events.
- Version-1 event subjects and envelope context are now registered in
  `contracts/events/arda-events-v1.json`; CI checks the registry against the
  shared Go event constants and keeps reference-only consumers explicit.
- Event envelopes include request/trace/traceparent, organization and actor
  context from the verified outgoing metadata when an outbox row is created.
- Event envelope ID generation now fails the publish/build operation when secure
  randomness is unavailable; it no longer falls back to a predictable timestamp
  identifier.
- Tracked development credentials and hard-coded database/cache URLs are removed
  from BE defaults, examples and Compose configuration. `scripts/check-secrets.mjs`
  is a CI gate; runtime rotation and secret-manager delivery remain environment
  gates.
- Auth-gateway dotenv loading is opt-in through `ARDA_LOAD_DOTENV=true`; a
  production process no longer searches parent directories for an implicit
  `.env` fallback.
- Every HTTP service entrypoint now exposes an internal aggregate `/metrics`
  endpoint and propagates a validated W3C `traceparent`; the wrapper preserves
  streaming `Flush`, hijacking and reader optimizations without using URL or
  tenant IDs as metric labels.
- Workflow case creation and submission preserve `Idempotency-Key` through the
  HTTP, CRM/HRM, gRPC and repository boundaries. Case keys are unique within a
  tenant, and submission commands use a PostgreSQL advisory lock per
  tenant/case to serialize concurrent retries before the Zeebe transition.
  Finance transactions and Workflow cases now persist request hashes and reject
  same-key/different-payload replays with conflict semantics.
- The external idempotency header is now standardized as `Idempotency-Key` for
  Finance as well; body fields remain explicit command inputs and are not
  guessed from a legacy `X-Idempotency-Key` alias.
- IAM management target scope is now explicit end-to-end: admin user, group and
  role path operations require `tenant_id`; repository CRUD and RBAC mappings
  enforce the target tenant, and repository role/group mappings no longer widen
  a lookup to the legacy `default` tenant. `/me` self-service lookups remain
  actor-bound and are not conflated with management target selection; the
  reserved bootstrap tenant is used only by explicit system provisioning.
- Workflow role memberships are now explicit target-tenant management data:
  membership list/create/update requires `tenant_id`, repository operations
  filter by that tenant, work-item claim-role resolution uses the case tenant,
  and workflow task get/list/claim paths retain verified tenant predicates. The
  historical empty-string membership default is dropped for new writes; existing
  empty rows remain a deliberate data-assignment gate rather than being guessed.
- Workflow delegations are now explicit tenant-owned management data: the API,
  FE and repository require `tenant_id`, reads and updates are tenant-predicated,
  and migration `20260825165000_scope_workflow_delegations.sql` adds the
  non-empty check as `NOT VALID`; historical delegation rows must be assigned
  by an operator before validation and `NOT NULL` hardening.
- Workflow role catalog, assignment rules and process definitions remain
  service-global by schema and seed model. Their global ownership is explicit
  in the data catalog; management endpoints require `workflow.manage`.
- Platform gRPC parameter resolution and organization list/create now bind request
  tenant fields to the verified incoming workload tenant; an internal caller can
  no longer enumerate or write platform organizations by omitting scope.
- Platform repository list/get/delete and tenant-owned create/update boundaries
  now reject empty tenant IDs independently of HTTP/gRPC handlers; global geo
  data remains outside that tenant-owned contract.
- New Finance and Platform migrations drop synthetic tenant defaults and add
  non-empty tenant checks for tenant-owned tables. Checks are `NOT VALID` by
  design until existing historical `default` rows receive an operator-approved
  tenant assignment; the migration does not fabricate that mapping.
- New CRM and Notification migrations also remove tenant defaults for customer
  records and notification outbox records. Their checks reject empty and
  reserved `default` tenant values; existing rows remain an explicit assignment
  gate before validation.
- Notification public-id reads and push-subscription cleanup now include the
  tenant predicate; notification service user context rejects the reserved
  `default` tenant before repository access.

## Verification recorded

- Go: the full go.work module loop passes for every backend module, including
  auth-gateway, all domain services, shared libraries and tools.
- The workflow target-tenant guard and platform repository tenant-boundary unit
  tests pass alongside the domain service suites.
- FE: package-boundary validation reports 17 workspaces with no cycles, and all
  app/package workspace typechecks pass.
- FE transport/list/telemetry regression tests pass (`30 pass`), including the browser
  cookie invariant, canonical problem parsing, idempotency header propagation,
  request-ID preservation across step-up retry and stable list query keys.
- Hygiene: `git diff --check` reports no whitespace errors in backend, frontend
  or infrastructure repositories.
- Additional gates: OpenAPI and migration-standard scripts pass; notification
  outbox consumer/DLQ reference code compiles and its service tests pass;
  Kustomize render passes. Cluster rollout, mTLS handshake, canary, backup/
  restore and replay evidence are not available from the current kubeconfig.
- OpenAPI coverage now includes separate versioned IAM and browser-auth
  documents; the checker validates every JSON contract rather than only the
  original IAM pilot. The media contract now separates multipart upload from
  the scoped presigned init/complete lifecycle, binary view and redirect
  download profiles; internal attachment is excluded from public REST because
  its protobuf gRPC contract is the canonical command. Public media responses
  omit internal database and object-storage coordinates.
- MFE adds a static credentialed-fetch gate so future auth refactors cannot
  accidentally remove `credentials: "include"` from `/api/auth/me`, raw
  Kratos/OAuth calls or other browser session requests.
- MFE also rejects known API-to-empty-data fail-open patterns in CI and the
  Workflow configuration pages now retain an explicit error/retry state when
  those loads fail.
- MFE browser runtime, unhandled-rejection and remote-module errors now use one
  bounded/redacted telemetry hook; its shell integration and redaction test pass.
- A direct-to-target k6 harness now uses fixed `TARGET_RPS`, preserves optional
  authenticated cookies without logging them, and records server waiting time
  separately from total request latency; it is ready for staging evidence but
  has not been run against production from this worktree.
- A read-only SSH audit now has live cluster evidence in
  `runtime-evidence-20260825.md`: the control planes and platform dependencies
  are healthy, while the deployed backend is Degraded because HRM lacks a
  database secret key, one auth-gateway pod crashes, mTLS Secrets are absent,
  observability is not in the live image, and historical tenant rows still need
  owner-approved assignment.
- A read-only unauthenticated smoke against the public edge on 2026-08-25
  returned `401` for `/api/auth/me` and the admin permissions route, with
  `cf-cache-status: DYNAMIC` and HKG CF-RAYs. It is edge/auth evidence only;
  it does not close authenticated origin, DB, canary or load gates.
- The smoke exposed that the currently deployed auth boundary can return a
  body `request_id` different from the response `X-Request-Id`. The shared
  problem writer now always binds body and header to the current request
  boundary; deployment verification must confirm the running image contains
  this fix.

## Still open before phase gates

- Complete endpoint/consumer/data owner validation and accept ADR decisions.
- Migrate the remaining IAM auth/OAuth/Kratos protocol-owned payloads only
  through dedicated adapters; their provider response schemas must not be
  changed by generic JSON rewrites.
- Provision and rotate the mTLS certificate Secrets in each environment, then
  capture handshake and cross-service gRPC evidence; code no longer accepts
  plaintext, but runtime certificate evidence is still a deployment gate.
- Finish the remaining domain repository scope audits, submission crash-recovery
  evidence, event consumer/inbox/DLQ/replay integration evidence, and generated
  OpenAPI/FE client conformance. Global workflow configuration repositories are
  now explicitly classified; tenant-owned paths have boundary predicates.
- Add runtime canary, load, backup/restore and rollback evidence before removal
  of legacy routes or compatibility adapters.
- Configure the production telemetry exporter and verify browser-to-service
  correlation; source-side FE-09 is landed but an exporter is intentionally not
  invented in the frontend repository.

### Additional source gates completed in this pass

- Session persistence is now an explicit mode: `redis` is the default and a
  missing `REDIS_URL` fails startup; `memory` is available only when explicitly
  selected for local development. There is no `REDIS_URL`-empty fallback.
- Notification outbox startup now requires NATS JetStream and exits when the
  broker or stream is unavailable; local Compose provisions a durable NATS
  JetStream volume and runtime Kubernetes requires `NATS_URL`.
- Finance startup now requires the Platform gRPC parameter resolver and exits
  when the mTLS/workload-authenticated dependency cannot be dialed. Finance is
  included in the local Compose topology.
- Compose build contexts are aligned with the monorepo Dockerfiles so local
  builds use the same source tree and Dockerfile mapping as the image workflow.
- Removed the unused IAM Redis policy-watcher/memory adapter instead of keeping
  an unconnected in-memory policy path.
