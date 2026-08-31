# Security and permissions

## Trust boundaries

The browser is an untrusted client. The gateway-injected context is trusted only
because the internal boundary authenticates the gateway/service call. The model
is untrusted input/output and never a security principal.

The AI service must authenticate internal calls with the existing Arda service
identity/mTLS or approved service-auth mechanism. Network placement alone is not
authorization.

## Permission model

Use explicit permission codes owned by IAM, for example:

- `ai.assistant.use` — start a conversation/run;
- `ai.knowledge.read` — retrieve approved knowledge (implemented for the first
  read-only slice);
- `ai.tool.<domain>.<operation>` — invoke a named tool;
- `ai.approval.execute` — approve a particular action class.
- `ai.approval.propose` — create a typed, non-executing proposal (implemented
  behind a disabled-by-default feature flag).

These names are proposed and require alignment with IAM permission conventions.
Do not grant `ai.*` wildcard access to normal users.

Every tool additionally checks resource and tenant scope. A permission check at
the gateway is necessary but not sufficient for a tool or domain mutation.

## Risk policy

Use the existing auth risk model:

- low: local/session validation and bounded context cache may be acceptable;
- medium: fresh context when stale and explicit tool policy;
- high: fresh IAM check, recent-auth, configured MFA/step-up, domain approval,
  and immutable audit.

AI reads that expose financial, HR, or identity data should default to medium or
high until classified. A prompt cannot lower route/tool risk.

## Secrets and provider controls

- Store model credentials only in the cluster secret mechanism and inject them
  into the AI service; never commit them or send them to the MFE.
- Allowlist provider hosts and models; set timeouts, token budgets, and cost
  limits.
- Log provider/model IDs and usage metadata, not prompts containing secrets.
- Do not send more data to a provider than the selected tool/RAG policy allows.
- Define provider outage behavior before enabling production traffic.

## Fail-closed requirements

Reject the request when actor, tenant, permission, service authentication,
approval, or source ACL is missing or ambiguous. A model timeout, provider
error, or audit write failure must never turn into a successful mutation.

## Security test cases

- spoofed `X-User-*`, tenant, role, permission, or auth headers;
- cross-tenant conversation ID, source ID, and resource ID;
- revoked permission/auth version during a paused approval;
- prompt injection in a knowledge document;
- tool schema extra fields, oversized inputs, replayed idempotency keys;
- provider timeout, partial stream, browser reconnect, and duplicate resume;
- redaction checks for logs, audit, transcript, metrics, and error responses.

## Service-to-service: caller identity vs delegated subject

Every tool dispatch from ai-service to a domain service (CRM, IAM, finance,
...) crosses an internal boundary. Two facts are kept separate on that
boundary:

- **Caller identity** — *which service* is calling. Signed with the shared
  `ARDA_SERVICE_AUTH_SECRET` as a short-lived `x-service-auth` HMAC assertion
  (`libs/go/arda-grpc/identity`). Never derived from request arguments.
- **Delegated subject** — *which user/tenant/org the caller acts for*.
  Forwarded as `X-User-Id`, `X-Tenant-Id`, `X-Org-Id`, `X-User-Org-Ids`,
  `X-Roles`, `X-Permissions`, `X-Auth-Checked` headers
  (`libs/go/arda-grpc/metadata`). Built **only** from the gateway-verified AI
  scope (`scopeToMetadata` in `apps/ai-service/internal/catalog`); tool
  arguments can never set these headers.

```
Gateway → user auth → ai-service
                            │
              ┌─────────────┴─────────────┐
         Caller identity            Delegated subject
         ai-service (signed)        user/tenant/org (Context)
              └──────────┬──────────────┘
                         │ svcclient (typed clients)
                         │ x-service-auth + subject headers
                         ▼
              CRM / IAM / Finance  ← /internal/ai/* routes
                         │ RequireServiceAuth (strict)
                         ▼
              resource-level authorization (target tự check)
```

### Contract

1. **`/internal/ai/*` is hard-closed.** Missing, invalid, expired, or
   wrong-audience `x-service-auth` → `401`; caller not in the allowed-source
   set → `403`. There is no fallback or pass-through
   (`identity.RequireServiceAuth`). Precedent: IAM's existing
   `internalService` for `auth-gateway`.
2. **The subject comes from the verified context, not from the client.** The
   dispatcher converts `tools.Context` (gateway-injected) into
   `metadata.Context`; typed clients (`svcclient`) reject any attempt to
   override identity headers from tool arguments by construction.
3. **Replay protection is a bounded window, not a one-time token.** Tokens
   carry `source + audience + issued/expiry + nonce` and are valid ≤ 5
   minutes; there is no server-side replay store yet. On one cluster this
   reduces replay risk without per-service secrets; the nonce exists for
   future audit/correlation. Recorded limitation, not a design goal.
4. **`X-Permissions`/`X-Roles` are informational hints.** The ai-service
   re-checks `RequiredPermissions` before dispatch, but the *target service*
   remains the authority: each internal handler re-validates tenant/org/user
   scope (e.g. IAM `requiredAdminTargetTenant`, CRM `getCustomer` scoping)
   and performs resource-level authorization on the delegated subject.
5. **Retry policy is method-aware.** `svcclient.Do` auto-retries idempotent
   methods (GET/HEAD/OPTIONS) once on transient errors/5xx; POST/PUT/PATCH
   are never auto-retried. Responses are size-bounded (`MaxResponse`).
6. **Mutations stay human-in-the-loop.** `Kind: confirm` SDK methods produce
   an `ApprovalProposal`; the dispatcher never executes them directly.

### Checklist: thêm service mới

- [ ] Tạo `svcclient/<service>.go`: `New<Service>Client(baseURL, "ai-service",
      secret, hc)` + method typed cho từng SDK read/confirm.
- [ ] Đăng ký catalog trong `RegisterBuiltinCatalog` (domain registrar mới
      hoặc thêm vào registrar hiện có).
- [ ] Thêm route `GET /internal/ai/...` trong service đích với
      `identity.RequireServiceAuth(secret, "<service>", identity.AllowedSources("ai-service"))`.
- [ ] Handler internal route **re-validate tenant/org/user scope** từ delegated
      headers (không tin `X-Permissions`).
- [ ] Response dùng envelope chuẩn (`{result: ...}`) để typed client decode
      bounded; không trả raw model.
- [ ] Khai báo `ARDA_SERVICE_AUTH_SECRET` trong manifest/deployment của cả
      ai-service và service đích.
- [ ] Test: token đúng → 200, thiếu → 401, sai source → 403, response vượt
      `MaxResponse` → error.
