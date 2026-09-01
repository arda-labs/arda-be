# Domain onboarding runbook — expose a domain capability to the AI assistant

> **Status (2026-09-01): validated end-to-end by the hrm pilot** —
> `arda.hrm.listEmployees` went from handler (`hrm-service`) through
> `hrm-v1.json` → `catalog-gen` → ai-service registration with zero
> ai-service code changes (only config/ClientSet wiring, one-time per new
> service, not per tool). `scripts/check-ai-catalog.mjs` now also fails when
> a contracted service is missing `<PREFIX>_SERVICE_URL` in the arda-infra
> ai-service Deployment (sibling checkout present only).

Goal: a domain team (hrm, crm, finance, workflow, ...) adds one AI tool for
Olorin **without touching ai-service code**. End to end this is a commit in
the domain service + a commit in `contracts/ai-internal/` + one line in
`arda-infra`. Expected effort after the first pass: **half a day including
tests**.

The pipeline (see `catalog-scale-plan.md` WP5):

```
/internal/ai/* handler in your service          contracts/ai-internal/<domain>-v1.json
        │  signed caller + delegated subject          │  x-ai-tool annotation
        ▼                                             ▼
   registered at startup  ◄──  generated.go  ◄──  go run ./tools/catalog-gen
                                     │
                        ai-service registry → SDK types → search()
                        → sandbox execute → permission check → your handler
```

## Checklist

### 1. Internal handler in your service

Add a `/internal/ai/...` route behind the `internalAIService` middleware
(copy the pattern from `apps/crm-service/internal/transport/http/router.go`):

- The middleware rejects everything that is not a signed `ai-service` caller
  (`RequireServiceAuth` + `AllowedSources("ai-service")`). The route is never
  browser-reachable.
- Re-validate the delegated subject your handler cares about (tenant at
  minimum — see `identity.RequireServiceAuth` header injection and the
  `ardametadata.FromOutgoing(ctx)` tenant guard in the repository layer).
- Return the canonical envelope `{"result": <payload>, "success": true}` and
  redact sensitive fields in the handler — the contract allowlist is defense
  in depth, not the primary redactor.
- GET for reads; POST/PUT/DELETE for mutations (they automatically become
  confirm-kind tools that require human approval in Olorin).

### 2. Annotate the contract

Create or extend `contracts/ai-internal/<domain>-v1.json`:

- One operation per tool, with `x-ai-tool` (copy the shape from
  `iam-v1.json`). Required: `sdkPath` (`arda.<domain>.<method>`), `domain`,
  `service` (`<domain>-service`), `kind`, `risk`, `keywords`,
  `requiredPerms`, `returns`.
- Bind each parameter: `x-ai-arg` names the SDK argument, `x-ai-scope:
  tenant` sources a query param from the verified actor scope (never from
  tool arguments), `x-ai-transform: upper` normalizes casing.
- Declare the 200 response schema — it IS the redaction allowlist. Every
  field the assistant may see must appear there; everything else is dropped.
- Every `requiredPerms` ID must exist in
  `apps/auth-gateway/configs/policy.yaml`. A permission nobody holds makes
  the tool invisible; the CI check fails on it.

### 3. Permissions & policy

- If the permission already exists in `policy.yaml` (e.g. `hrm.read`), reuse
  it. Otherwise add it to the permission list of the relevant routes there
  and follow the auth-gateway process for registering the new permission ID.
- Remember the gateway `policy.yaml` route entry is only needed if the
  *browser* surface changes — internal AI calls do not go through
  auth-gateway.

### 4. Regenerate the catalog

```
go run ./tools/catalog-gen        # regenerates apps/ai-service/internal/catalog/generated.go
go run ./tools/catalog-gen --check  # what CI runs; must pass
```

Commit `generated.go` in the same PR as the spec — CI fails if it is stale.

### 5. Deployment wiring (two places — the 2026-09-01 lesson)

- **ai-service Deployment** (`arda-infra/k8s/apps/ai-service.yaml`): add
  `<DOMAIN>_SERVICE_URL: http://<domain>-service:8080`. A missing URL means
  the tool silently does not register (deployment rule: unwired service =
  invisible tool, not broken tool).
- **Your service's Deployment**: nothing new if it already runs; confirm the
  `ARDA_SERVICE_AUTH_SECRET` env is present (service-to-service signing).
- Commit both to `arda-infra` — `kubectl edit` is overwritten by Argo CD
  selfHeal.

### 6. Test

- Unit test in your service: the internal handler serves the redacted shape
  under a valid service-auth + delegated-subject request (see
  `apps/iam-service` admin handler tests for the signed-request pattern).
- The generated catalog tests in ai-service cover dispatch/redaction
  generically; no ai-service code change is needed or expected.

### 7. Verify in the assistant (go-live)

1. Deploy, then in Olorin ask for the capability (e.g. "liệt kê nhân viên
   phòng kinh doanh") with an account that holds the permission.
2. Also check with an account **without** the permission — the tool must be
   invisible/forbidden, not leaked.
3. If the tool does not appear: check `check-ai-catalog` locally, then the
   ai-service pod env (`env | grep SERVICE_URL`), then the ai-service logs.

## Anti-patterns

- Do NOT add a typed client in ai-service for a new tool — generated entries
  ride the generic executor. Typed clients remain only for legacy entries.
- Do NOT expose aggregate/report endpoints that need orchestration across
  services as a single `x-ai-tool` — put those in the hand-written catalog
  (`internal/catalog/`) instead, or compose in the sandbox.
- Do NOT widen `requiredPerms` to reuse an unrelated permission "temporarily".
