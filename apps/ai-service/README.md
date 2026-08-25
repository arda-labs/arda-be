# AI service protocol spike

This is Gate 1 of the Arda AI rollout. It is intentionally a deterministic
AG-UI-compatible HTTP/SSE endpoint with no database, model provider, tool, or
production deployment.

Run locally:

```powershell
go run ./cmd/ai-service
```

Endpoint: `POST /api/ai/agent`

The endpoint requires gateway-derived `X-Auth-Checked: true`, `X-User-Id`,
`X-Tenant-Id`, and `X-Permissions: ai.assistant.use`. It must be reached through
the authenticated gateway in an environment where it is deployed; these headers
are not a standalone authentication mechanism.

The response is an AG-UI-style SSE stream with `RUN_STARTED`, text message
events, and `RUN_FINISHED`. The implementation deliberately does not persist
the request or echo user content.

For the shell page, start the frontend with `VITE_AI_PROTOCOL_SPIKE=true` and
run the gateway with `AI_SERVICE_URL=http://localhost:8098`. The gateway still
requires a real authenticated session and the `ai.assistant.use` permission;
setting the frontend flag does not bypass either check.
