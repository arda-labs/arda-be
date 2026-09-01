# Internal AI surface contracts

Each document describes the `/internal/ai/*` operations a domain service
exposes to `ai-service` over the signed service-to-service transport. These
are **not** browser APIs: they authenticate with `x-service-auth` assertions
and carry delegated subject headers, so the public-contract invariants in
`contracts/openapi/` (cookie credentials, `ResponseMeta`/`Problem`) do not
apply here.

Every operation that ai-service may call on behalf of a user MUST be
annotated with an `x-ai-tool` extension. `tools/catalog-gen` turns these
annotations into `apps/ai-service/internal/catalog/generated.go` — the
committed, auditable catalog the assistant discovers and executes.

## Adding a tool (the WP5 workflow)

1. Implement the `/internal/ai/...` handler in the domain service behind the
   `internalAIService`-style middleware (signed caller + delegated subject).
2. Annotate the operation in `contracts/ai-internal/<domain>-v1.json` with
   `x-ai-tool` (see existing files for the shape): `sdkPath`, `domain`,
   `kind` (`read` for GET, `confirm` for mutations), `risk`, `timeoutMs`,
   `keywords`, `requiredPerms`, `service`, `query`/`scopeQuery` bindings,
   `envelope`, `returns`, `responseSchema`.
3. Ensure every `requiredPerms` ID exists in the permission registry
   (`apps/auth-gateway/configs/policy.yaml`). A permission nobody can hold
   makes the tool invisible to everyone — the CI check fails on this.
4. Run `go run ./tools/catalog-gen` and commit the regenerated
   `generated.go` in the same PR. CI fails if it is stale.

## Redaction is declarative

`responseSchema` is an allowlist: the generic executor drops any response
field not declared there (recursively through objects and arrays). Never
list a field in `responseSchema` that the owning service has not already
redacted — this layer is defense in depth, not the primary redactor.
