# AI service

The first production boundary of the Arda AI rollout: an AG-UI-compatible
HTTP/SSE endpoint with persistent conversations and runs, an optional
model-driven agent loop, allowlisted tools, server-enforced human approval,
and owner-scoped conversation APIs.

With read tools enabled, production exposes `crm.customer.get` and
`knowledge.search`. CRM requires `crm.customer.read`; knowledge requires the
separate `ai.knowledge.read` permission. Both use server-resolved tenant scope
and return bounded, redacted data; knowledge results include citations.

HITL endpoints are guarded by `AI_ENABLE_HITL_PROPOSALS` and remain disabled
in the production manifest. When enabled, `confirm`-kind tools (currently
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

Set the following to enable the model-driven agent loop:

```dotenv
AI_ENABLE_AGENT=true
AI_MODEL_BASE_URL=https://api.openai.com/v1
AI_MODEL_API_KEY=<provider key, secret via K8s secretKeyRef only>
AI_MODEL_ID=<model id>
AI_AGENT_MAX_STEPS=6
AI_RATE_LIMIT_PER_MINUTE=30
```

Knowledge ingestion uses an OpenAI-compatible embedding endpoint with a
1024-dimension vector. Set `AI_RAG_EMBEDDING_BASE_URL`,
`AI_RAG_EMBEDDING_API_KEY`, and `AI_RAG_EMBEDDING_MODEL` explicitly when the
embedding provider differs from chat; they fall back to the chat URL/key for
backward compatibility. Production mode fails closed when embeddings are
missing; development can opt into the same behavior with
`AI_RAG_REQUIRE_EMBEDDING=true`. Optional Cohere-compatible reranking is
enabled with `AI_RAG_RERANKER_BASE_URL`, `AI_RAG_RERANKER_API_KEY`, and
`AI_RAG_RERANKER_MODEL`. Optional multi-query rewrite asks the tenant model for
up to two additional Vietnamese search queries and fuses the result sets with
RRF; it is enabled by default and can be disabled with
`AI_RAG_QUERY_REWRITE=false`.

The provider must speak the OpenAI-compatible chat-completions SSE protocol
(cloud providers, vLLM, Ollama, and similar local runtimes all work). The
handler depends only on the `model.Provider` interface, so additional sources
can be added later without touching tool or handler code. Model configuration
is tenant-owned (AI Settings UI): each tenant stores one active base URL, API
key and model id in `ai_tenant_settings`; the deployment only supplies the
shared AI Gateway token and an optional base-URL allowlist. Repeated upstream
failures are circuit-broken. The agent loop
streams `TEXT_MESSAGE_*` deltas incrementally, executes only registry tools
whose permissions resolve against gateway headers, and never executes
`confirm`-kind tools directly: requesting one creates an approval proposal
and ends the run in `WAITING_APPROVAL`.

## Endpoints

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
violates its expected evidence or no-answer policy.

For the end-to-end gateway check, provide a short-lived authenticated session
cookie and run `node scripts/gateway-smoke.mjs`. The script verifies the SSE
terminal event and monotonic sequence without invoking mutation tools.
