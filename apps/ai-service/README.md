# AI service protocol boundary

This is the first production boundary of the Arda AI rollout. It is a
deterministic AG-UI-compatible HTTP/SSE endpoint with persistent conversations
and runs, two read-only tools, but no model provider or mutation capability.

Production enables `crm.customer.get` and `knowledge.search` when read tools
are enabled. CRM requires `crm.customer.read`; knowledge requires the separate
`ai.knowledge.read` permission. Both use server-resolved tenant scope and
return bounded, redacted data; knowledge results include citations.

HITL proposal endpoints are guarded by `AI_ENABLE_HITL_PROPOSALS` and remain
disabled in the production manifest. When enabled in a non-production
environment, the only proposal is a typed `crm.customer.export.prepare` record;
it creates no export and has no execution/resume path.

Run locally:

```powershell
AI_MODE=spike go run ./cmd/ai-service
```

Endpoint: `POST /api/ai/agent`

The endpoint requires gateway-derived `X-Auth-Checked: true`, `X-User-Id`,
`X-Tenant-Id`, and `X-Permissions: ai.assistant.use`. It must be reached through
the authenticated gateway in an environment where it is deployed; these headers
are not a standalone authentication mechanism.

The response is an AG-UI-style SSE stream with `RUN_STARTED`, text message
events, and `RUN_FINISHED`. In production mode `DATABASE_DSN` and
`ARDA_SERVICE_AUTH_SECRET` are mandatory; migrations run at startup and the
gateway supplies a separate short-lived workload identity.

For the shell page, start the frontend with `VITE_AI_PROTOCOL_SPIKE=true` and
run the gateway with `AI_SERVICE_URL=http://localhost:8098`. The gateway still
requires a real authenticated session and the `ai.assistant.use` permission;
setting the frontend flag does not bypass either check.
