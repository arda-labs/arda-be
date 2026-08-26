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
AI_MODE=spike go run ./cmd/ai-service
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

The provider must speak the OpenAI-compatible chat-completions SSE protocol
(cloud providers, vLLM, Ollama, and similar local runtimes all work). The
handler depends only on the `model.Provider` interface, so additional sources
can be added later without touching tool or handler code. The agent loop
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

The endpoint requires gateway-derived `X-Auth-Checked: true`, `X-User-Id`,
`X-Tenant-Id`, and `X-Permissions: ai.assistant.use`. It must be reached through
the authenticated gateway in an environment where it is deployed; these headers
are not a standalone authentication mechanism.

The response is an AG-UI-style SSE stream with `RUN_STARTED`, text message
events, and `RUN_FINISHED`. In production mode `DATABASE_DSN` and
`ARDA_SERVICE_AUTH_SECRET` are mandatory; migrations run at startup and the
gateway supplies a separate short-lived workload identity.

For the shell panel, start the frontend with `VITE_AI_ENABLED=true` (or the
legacy `VITE_AI_PROTOCOL_SPIKE=true`) and run the gateway with
`AI_SERVICE_URL=http://localhost:8098`. The gateway still requires a real
authenticated session and the `ai.assistant.use` permission; setting the
frontend flag does not bypass either check. Without a model configured the
endpoint stays deterministic and answers with the protocol spike message.
