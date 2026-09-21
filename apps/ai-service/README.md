# AI service

The first production boundary of the Arda AI rollout: an AG-UI-compatible
HTTP/SSE endpoint with persistent conversations and runs, an optional
model-driven agent loop, allowlisted tools, server-enforced human approval,
and owner-scoped conversation APIs.

With Code Mode enabled (`AI_ENABLE_READ_TOOLS=true`, the production value) the
model only sees three meta-tools — `search`, `execute`, `readResult` — and
reaches the typed `arda.*` SDK inside the Goja sandbox. Permission checks run
per SDK method (`ai.knowledge.read` for knowledge, `crm.customer.read` for CRM,
…), results stay bounded and redacted, and knowledge results include citations.

HITL endpoints are guarded by `AI_ENABLE_HITL_PROPOSALS` (enabled in the
production manifest). `confirm`-kind tools (currently
`crm.customer.export.prepare`) can never execute directly: the agent loop
turns them into persisted approval proposals, an independent approver decides,
and only the run owner can trigger execution afterwards. `prepare` still
creates no export artifact; it verifies scope and returns a bounded payload.

Run locally:

```powershell
$env:AI_MODE="development"
go run ./cmd/ai-service
```

## Agent mode (model provider)

Model credentials are tenant-owned: configure a profile in the AI Settings UI
(`/ai/settings`). The service resolves the applied profile + model per tenant;
production never reads model credentials from deployment env. The deployment
supplies only shared controls:

```dotenv
AI_AGENT_MAX_STEPS=6
AI_RATE_LIMIT_PER_MINUTE=30
AI_MODEL_GATEWAY_TOKEN=<shared AI Gateway token, secret via K8s secretKeyRef>
AI_MODEL_BASE_URL_ALLOWLIST=https://ai-gateway.arda.io.vn/compat
```

Knowledge ingestion uses an OpenAI-compatible embedding endpoint with a
1024-dimension vector. Set `AI_RAG_EMBEDDING_BASE_URL`,
`AI_RAG_EMBEDDING_API_KEY`, and `AI_RAG_EMBEDDING_MODEL` explicitly; embedding
configuration is independent of the tenant chat model. Production mode fails
closed when embeddings are missing; development can opt into the same behavior
with `AI_RAG_REQUIRE_EMBEDDING=true`. Embedding calls retry once within the
caller deadline (the deadline is split between attempts so one provider spike
cannot consume it), and query vectors are cached for
`AI_RAG_EMBEDDING_CACHE_TTL_SECONDS` (default 6h; Redis with an in-process
fallback, 0 disables). Optional Cohere-compatible reranking is
enabled with `AI_RAG_RERANKER_BASE_URL`, `AI_RAG_RERANKER_API_KEY`, and
`AI_RAG_RERANKER_MODEL`. Optional multi-query rewrite asks the tenant model for
up to two additional Vietnamese search queries and fuses the result sets with
RRF; it is enabled by default and can be disabled with
`AI_RAG_QUERY_REWRITE=false`. The rewrite is best-effort and runs concurrently
with the primary query under a bounded budget (3s, raised from 1.5s after
production rewrites never completed in time): a slow model degrades
recall instead of failing the call, and rewrite variants are skipped when the
request deadline no longer leaves room for an embedding round-trip plus a
search. Deadline-bound callers (the Code Mode tool) wait only 300 ms for the
rewrite before answering with the primary hits; the standalone RAG endpoint and
eval runs wait for the whole rewrite budget to maximise recall. Inside the Code Mode sandbox the caller deadline bounds the whole
retrieval: `knowledge.search` declares 8 s (external embedding spikes above 3 s
are measured) under the 10 s `execute` ceiling, while the sandbox default for
deadline-less callers stays 3 s.

The provider must speak the OpenAI-compatible chat-completions SSE protocol.
AI Settings stores a server-owned provider preset alongside each profile:
`openai`, `openai-compatible`, `opencode-go`, `ollama`, or `vllm`. Presets do
not allow arbitrary custom headers. In particular, `opencode-go` uses
`https://opencode.ai/zen/go/v1` for chat-completions models and the service
adds an opaque, stable `x-opencode-session` plus its own User-Agent for every
conversation. Anthropic Messages and OpenAI Responses models need their own
wire adapters and are intentionally not selectable as chat-completions
profiles yet. Model configuration is tenant-owned (AI Settings UI): each
tenant stores one applied profile/model, while the deployment supplies the
shared AI Gateway token and an optional base-URL allowlist. Repeated upstream
failures are circuit-broken. The agent loop
streams `TEXT_MESSAGE_*` deltas incrementally, executes only registry tools
whose permissions resolve against gateway headers, and never executes
`confirm`-kind tools directly: requesting one creates an approval proposal
and ends the run in `WAITING_APPROVAL`.

## Endpoints

### Identity answers

`arda.iam.me()` keeps the gateway identity fields and adds display labels:
`user.name`, `tenant.name/code`, `organizationDetails` (id/name/code), and
`displayResolution` (`resolved`, `partial`, or `unavailable`). The original
`organizations` array remains an array of IDs. Tenant/user labels come from
`iam.getMyDisplayContext`, whose signed internal IAM endpoint verifies the
actor's active tenant membership and only exposes that tenant's labels.
Organization labels use `platform.listOrganizations`, require `platform.read`,
and match the actor's organization IDs exactly. Both nested reads honor catalog
governance. Organization lookup stops after three pages of 20; the whole
enrichment has a 4.5-second budget. Lookup failures preserve identity and report
unresolved labels; they never manufacture names or broaden tenant scope.

The default prompts prefer names and business codes, summarize roles, and
avoid dumping permission codes unless requested. `AI_MODEL_SYSTEM_PROMPT`
still replaces the default prompt when explicitly configured. Both IAM and AI
service changes must be deployed to enable tenant labels; older IAM deployments
leave these labels unavailable. No public gateway route or MFE API is added.
Answer-level acceptance scenarios: [identity answer evaluation](../../docs/ai/identity-answer-evaluation.md).

### HTTP routes

- `POST /api/ai/agent` — AG-UI SSE run.
- `GET /api/ai/conversations` — owner-scoped thread list (`limit` ≤ 100).
- `GET /api/ai/conversations/{threadId}/messages` — owner-scoped transcript (`limit` ≤ 500).
- `POST /api/ai/approvals` — HITL proposal (flagged).
- `POST /api/ai/approvals/{id}/decision` — independent approver decision (flagged).
- `POST /api/ai/approvals/{id}/execution` — run owner executes an APPROVED confirm tool; retries while the execution row stays `WAITING_APPROVAL`.
- `GET /health/live` and `/health/ready` — liveness is process-only; readiness
  checks the configured database when persistence is enabled.

The endpoint requires gateway-derived `X-Auth-Checked: true`, `X-User-Id`,
`X-Tenant-Id`, and `X-Permissions: ai.assistant.use`. It must be reached through
the authenticated gateway in an environment where it is deployed; these headers
are not a standalone authentication mechanism.

The response is an AG-UI-style SSE stream with `RUN_STARTED`, text message
events, and exactly one terminal event: `RUN_FINISHED` for success/interrupt or
`RUN_ERROR` for failure. Every event carries the additive Arda fields
`protocolVersion: "ag-ui-v1"`, a per-run `eventId`, and a monotonically
increasing `sequence`; the response advertises the same value in
`X-Arda-AI-Protocol-Version`. Clients may send `protocolVersion` in the run
input, and an unsupported value is rejected before the stream opens. A client
disconnect or request deadline is persisted as `CANCELLED`/`FAILED` and is
reported as `RUN_ERROR` with `ai.run_cancelled`/`ai.run_timeout` when the
connection can still be written. A run without a valid provider configuration
fails with `ai.model_unavailable`; the service never returns a successful placeholder
response. In production mode `DATABASE_DSN` and
`ARDA_SERVICE_AUTH_SECRET` are mandatory; migrations run at startup and the
gateway supplies a separate short-lived workload identity.

Rate limiting is per tenant/user per minute (`AI_RATE_LIMIT_PER_MINUTE`). Set
`REDIS_URL` to share the window across replicas; without it the in-process
token bucket applies per replica. Readiness fails when
`AI_RAG_REQUIRE_EMBEDDING` is on and no embedding provider is configured.

For the shell panel, start the frontend with `VITE_AI_ENABLED=true` and run the gateway with
`AI_SERVICE_URL=http://localhost:8098`. The gateway still requires a real
authenticated session and the `ai.assistant.use` permission; setting the
frontend flag does not bypass either check. Model credentials are configured
per tenant in AI Settings; the deployment only provides the shared gateway
token and allowlist.

Tenant quota settings may set `monthlyTokenLimit`. Each model run reserves a
bounded allowance atomically and finalizes it with provider usage; exceeding
the limit returns `ai.quota_exceeded` before the run starts.

## Retrieval evaluation

The repository includes a repeatable retrieval evaluator. Run it against a
local service or gateway with the same tenant identity used by the golden set.
For authenticated gateway use, set `AI_EVAL_COOKIE` (must be a short-lived
test session; never print or store it):

```powershell
$env:AI_EVAL_BASE_URL="http://localhost:8098"
$env:AI_EVAL_STRICT="1"
# For authenticated gateway use, set AI_EVAL_COOKIE (must be a short-lived test session; never print or store it):
# $env:AI_EVAL_COOKIE="<short-lived-test-session-cookie>"
go run ./cmd/ai-eval
```

It reports source recall, citation coverage, hit counts, latency, and a
machine-readable per-case result. A strict run exits non-zero when a case
violates its expected evidence or no-answer policy. The same binary also runs
the answer-level evaluation (`-mode=answer` or `AI_EVAL_MODE=answer`, set
`AI_ANSWER_EVAL_SET`/`AI_ANSWER_EVAL_STRICT`), which drives the full agent and
scores citations, source keys, keywords and no-answer behaviour. It ships in
the service image (`/app/ai-eval`) and runs nightly in-cluster via the
`ai-eval` CronJob in `arda-infra` (LAN-only clusters cannot be reached from
GitHub-hosted runners); the bundled golden set is a verified copy of
`scripts/ai-dev-corpus/evaluation-set.yaml` and
`scripts/check-ai-eval-set.mjs` fails CI when the two diverge.

For the end-to-end gateway check, provide a short-lived authenticated session
cookie and run `node scripts/gateway-smoke.mjs`. The script verifies the SSE
terminal event and monotonic sequence without invoking mutation tools.
