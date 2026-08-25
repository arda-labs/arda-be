# API Contracts and Frontend Standardization

## 1. Contract principles

The browser contract is HTTP semantics plus a small set of response profiles. It
is not one universal JSON wrapper for every protocol and content type.

Rules:

- HTTP status remains authoritative; errors are not encoded as HTTP 200.
- JSON field names use `snake_case` at the public REST boundary.
- Success bodies use one canonical envelope: `{result, success, errors, messages, meta}`.
- `result` contains the business resource/list/action result; operational metadata
  is separate in `meta`.
- Errors use `application/problem+json` compatible with RFC 9457.
- Binary, streaming, SSE, file transfer, and gRPC use their protocol-native shapes.
- Stable machine codes drive FE behavior and localization; backend messages are fallback text.

This target intentionally differs from the current flat response convention. A
migrated endpoint switches to this shape directly; it does not expose a permanent
runtime fallback or a query flag for two competing contracts.

## 2. HTTP success profiles

### 2.1 Single resource

Use for get, create, and update when the caller needs the resulting resource.

```json
{
  "result": {
    "id": "01...",
    "code": "ORG01",
    "name": "Ha Noi"
  },
  "success": true,
  "errors": [],
  "messages": [],
  "meta": {
    "request_id": "...",
    "trace_id": "..."
  }
}
```

- Create returns `201 Created` and `Location` when a stable resource URL exists.
- Read/update returns `200 OK`.
- `meta.request_id` and response `X-Request-Id` are the same value.

### 2.2 Offset-paginated list

Use for bounded administrative tables that require exact totals.

```json
{
  "result": {
    "items": [],
    "page": 1,
    "per_page": 20,
    "total": 0
  },
  "success": true,
  "errors": [],
  "messages": [],
  "meta": {
    "request_id": "...",
    "trace_id": "..."
  }
}
```

Rules:

- `page` is 1-based.
- `items` is always an array.
- `total` is after filters and before pagination.
- sort fields and filter values are allowlisted by resource.
- maximum `per_page` is declared in OpenAPI and enforced by the service.

### 2.3 Cursor-paginated list

Use for large or frequently changing collections where exact count is expensive
or offset stability is poor.

```json
{
  "result": {
    "items": [],
    "next_cursor": null,
    "has_more": false
  },
  "success": true,
  "errors": [],
  "messages": [],
  "meta": {
    "request_id": "...",
    "trace_id": "..."
  }
}
```

Cursor contents are opaque to clients. Ordering is stable and includes a unique
tiebreaker. `total` is omitted unless the use case truly requires it.

### 2.4 Command/action result

Use when an action has a meaningful result that is not a normal resource update.

```json
{
  "result": {
    "outcome": "accepted",
    "resource_id": "...",
    "state": "pending_approval"
  },
  "success": true,
  "errors": [],
  "messages": [],
  "meta": {
    "request_id": "...",
    "trace_id": "..."
  }
}
```

Do not return a generic `{ "success": true }` when a meaningful resource or
state can be returned. Use `204 No Content` when no body is needed.

### 2.5 Long-running operation

Commands expected to exceed the synchronous request budget return `202 Accepted`,
`Location: /api/operations/{id}`, and:

```json
{
  "result": {
    "id": "...",
    "status": "pending",
    "progress": null,
    "result": null,
    "error": null
  },
  "success": true,
  "errors": [],
  "messages": [],
  "meta": {
    "request_id": "...",
    "trace_id": "..."
  }
}
```

The operation resource has retention, polling, cancellation, and terminal-state
semantics defined per operation type.

### 2.6 Batch result

Use one overall HTTP status plus per-item results when partial success is a real
business requirement. Every item has a client correlation key and either `result`
or a problem-shaped `error`. Atomic batches must fail atomically instead.

### 2.7 Delete

- `204 No Content` when deletion or archival succeeds and no state is returned.
- `202 Accepted` when asynchronous cleanup follows.
- Repeated delete semantics are documented as idempotent success or not-found.

## 3. Non-JSON profiles

### Upload

1. Create upload session with JSON metadata.
2. Return upload ID, constraints, expiry, and presigned URL/parts.
3. Browser uploads bytes directly to object storage where allowed.
4. Complete upload through Media API.
5. Return the durable media resource; processing may continue through an operation/event.

Presigned URLs are secrets for their lifetime and must not be logged. Large files
use multipart upload with explicit abort/expiry cleanup.

### Download and streaming

Return bytes with appropriate `Content-Type`, `Content-Length`, `Content-Disposition`,
`ETag`, cache policy, and `Accept-Ranges`. Support `206 Partial Content` where required.
Do not wrap bytes in JSON or base64 unless an external contract specifically requires it.

### SSE

Use `text/event-stream`, stable event names and IDs, reconnect behavior, heartbeat,
authorization expiry behavior, and bounded replay semantics.

### gRPC

Use typed response messages and standard gRPC statuses/details. Do not copy the
HTTP `result/meta` wrapper into Proto messages.

## 4. Error contract

Content type: `application/problem+json`.

```json
{
  "type": "https://docs.arda.io.vn/problems/validation-invalid-input",
  "title": "Request validation failed",
  "status": 400,
  "code": "validation.invalid_input",
  "detail": "One or more fields are invalid",
  "instance": "/api/admin/users",
  "request_id": "...",
  "trace_id": "...",
  "errors": [
    {
      "field": "email",
      "code": "validation.email_invalid",
      "message": "Email is invalid"
    }
  ]
}
```

Rules:

- `code` is stable and versioned as part of the contract.
- `title` is short and stable; `detail` is safe fallback text, never sensitive internals.
- validation `field` uses the public request field/path, not a Go struct or DB column name.
- retry guidance is expressed by status, headers such as `Retry-After`, and documented code behavior.
- stack traces, SQL errors, upstream tokens and authorization details never reach clients.
- FE localization maps `code`; it does not branch on English text.

The error registry records code, owning domain, HTTP/gRPC mapping, retryability,
user visibility, audit relevance, and replacement/deprecation.

## 5. Request conventions

Every JSON endpoint declares:

- method and idempotency behavior;
- path resource name and target semantics;
- query/filter/sort allowlist;
- request schema and maximum body size;
- auth policy, scope and risk level;
- response profile and possible problem codes;
- concurrency precondition if applicable (`If-Match`/version);
- rate-limit class;
- audit event for sensitive commands.

Headers:

| Header | Rule |
| --- | --- |
| `Accept` | Client requests declared representation |
| `Content-Type` | Required for body; validated before decode |
| `Accept-Language` | UI/fallback locale only; not authorization input |
| `X-Request-Id` | Generated once by client or edge; validated and preserved |
| `traceparent`, `tracestate` | Standard distributed trace propagation |
| `Idempotency-Key` | Required for classified retryable commands |
| `If-Match` | Used for resources with optimistic concurrency |

Client-supplied identity, tenant, org, role, and permission headers are stripped
at the public boundary and replaced only with verified internal context.

## 6. OpenAPI and code generation

OpenAPI is the public REST source of truth after ADR-005. The repository structure
and generator are selected in Phase 2, with these required properties:

- specs are split by domain but linted as one public surface;
- reusable schemas cover response profiles and problem details;
- operation IDs are stable and unique;
- generated artifacts are reproducible and diff-checked in CI;
- breaking-change detection compares against the supported production contract;
- generation does not place business behavior in generated code;
- FE consumes generated request/response types or generated clients through domain adapters;
- backend conformance tests validate runtime responses against the published spec.

## 7. Frontend target layering

```text
page/components
  -> feature query/mutation hooks
  -> domain API adapter
  -> generated contract client/types
  -> @workspace/api transport
  -> browser fetch
```

Responsibilities:

| Layer | Owns | Must not own |
| --- | --- | --- |
| Components | Rendering, interaction, accessible state | URL construction, raw fetch, auth retry |
| Feature hooks | Query keys, cache invalidation, optimistic UI when safe | Transport headers or cookie policy |
| Domain adapter | Contract-to-view-model mapping, compatibility adapter | Global notifications or React rendering |
| Generated contract | Wire schemas and operations | Handwritten business rules |
| Shared transport | base URL, credentials, request/trace IDs, abort, decode, error normalization, step-up orchestration | Domain endpoint knowledge |

## 8. Frontend transport rules

- One shared client handles all JSON calls, including auth endpoints.
- Browser calls that depend on the BFF session, including `/api/auth/me`, always
  use `credentials: "include"`; this is an invariant tested in the shared
  transport. Protocol-native raw fetches (Kratos/OAuth) must declare the same
  credential policy explicitly.
- Production frontend/API origin, credentialed CORS, cookie `Secure`/`SameSite`
  settings and cookie domain/path must be verified together; changing only the
  frontend transport is not a valid auth refactor.
- Binary upload/download and SSE use explicit adapters built on the same base URL,
  credential, correlation, and error policy.
- Every request supports `AbortSignal`.
- A retry after step-up preserves the logical request ID and creates child trace spans.
- Automatic retry is limited to safe/idempotent operations and bounded by policy.
- A request that has already prompted step-up cannot recursively prompt again.
- Errors normalize to one typed `ApiProblem` before reaching features.
- No component reads arbitrary response shapes or displays raw backend errors.

## 9. Query, list and cache standards

- TanStack Query owns server state; local stores do not duplicate server resources.
- Query keys come from per-domain factories and contain canonical serialized params.
- URL state is the source for shareable admin list filters and pagination.
- Offset and cursor lists are different types and hooks.
- Table totals come from backend `total`, never current-page length.
- Quick search is debounced; advanced search distinguishes draft and applied filters.
- Mutations declare exact invalidation/update behavior.
- Optimistic updates are used only when conflict and rollback behavior are known.
- 401 triggers session handling; 403 is not treated as unauthenticated.
- `recent_auth_required` invokes one step-up flow and retries only the suspended mutation.

## 10. Forms, validation and errors

- Zod validates client input for UX, but backend validation remains authoritative.
- Form field mapping uses stable public field paths from problem `errors[]`.
- Business conflicts remain form-level or action-level problems, not fake field errors.
- User-facing strings come from i18n keys mapped from stable error codes.
- Request ID is available in the error UI for support/copy without exposing sensitive detail.
- Destructive/high-risk actions show target identity and require deliberate confirmation.

## 11. Auth and MFE integration

- Shell owns initial `/api/auth/me`, session expiry behavior, and StepUpProvider.
- Remotes consume normalized auth state and capability checks from `@workspace/auth`.
- Missing roles/permissions never imply access.
- Navigation visibility is a UX hint; backend policy is authoritative.
- Auth state includes verified tenant/org choices and capability metadata, not raw browser claims.
- Remote standalone development uses an explicit mock adapter, not production fail-open behavior.

## 12. Federation compatibility

- Pin and verify singleton versions from `federation.shared.ts`.
- Publish immutable remote assets and a versioned manifest.
- Test shell current/previous compatibility with each changed remote.
- A shared package contract change lands before dependent remote changes.
- Route-level preload and caching remain measured; refactor must not regress cold remote load.
- Remote telemetry includes remote name, version, route, load duration and error boundary outcome.

## 13. Frontend migration method

For each feature:

1. Add characterization tests for current wire behavior and UI decisions.
2. Publish/approve the target OpenAPI operation.
3. Add generated type/client and domain adapter.
4. Move feature calls from local/raw API code to the standard transport.
5. Move server state to standard query/list hooks where applicable.
6. Normalize validation and error presentation.
7. Verify auth, step-up, abort, loading, empty, partial and error states.
8. Enable through a compatibility switch or release sequence.
9. Observe and then remove the old adapter.

## 14. Frontend definition of done

- No raw JSON fetch in the migrated feature.
- Operation and response are contract-generated or contract-checked.
- Query key and invalidation are deterministic.
- Abort and unmount do not leave stale updates.
- 401, 403, recent-auth, validation, conflict, rate-limit and 5xx states are tested as applicable.
- Actor and target are clearly represented for management actions.
- Accessibility, i18n and responsive states pass the repository checks.
- Remote still builds independently and with the shell compatibility matrix.
- Request/trace identifiers are visible in telemetry and support errors.
- Legacy adapter removal is registered with an owner and phase.
