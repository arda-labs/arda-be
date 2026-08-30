# Go-native CopilotKit boundary (superseded)

> **SUPERSEDED (2026-08-31)** — replaced by the AG-UI protocol (see
> `agent-evolution-roadmap.md` §M1). `ai-service` now serves AG-UI events on
> `POST /api/ai/agent`; the CopilotKit envelope endpoint `/api/copilotkit` was
> removed. This document is kept as the historical record of the earlier
> CopilotKit-in-Go design.

The Node.js `ai-runtime` service was retired; `ai-service` had served the
CopilotKit envelope protocol directly from Go.

## What changed

Earlier phases deployed a separate internal Node service (`ai-runtime`) running
CopilotKit Runtime as an AG-UI adapter between the gateway and `ai-service`.
The adapter added a deployable, a secret boundary, and a second workload-token
hop without adding any authorization decision. It has been removed:

- `arda-be/apps/ai-runtime` source has been removed entirely; nothing deploys it.
- `arda-infra/k8s/apps/ai-runtime.yaml` was deleted from the kustomization;
  ArgoCD pruned the deployment (`kubectl get deploy ai-runtime` → NotFound).
- The gateway now points `COPILOTKIT_RUNTIME_URL` at
  `http://ai-service:8080` and signs its workload assertion for the audience
  `"ai-service"` (see `bff_handler.go`, `/api/copilotkit` prefix branch).

## Request chain

```text
Browser (session cookie, host-only on api.arda.io.vn)
  -> Cloudflare -> Tunnel -> Traefik
  -> auth-gateway /api/copilotkit/**
       policy id: ai-copilotkit-runtime (GET+POST+OPTIONS)
       permission: ai.assistant.use
       CORS handled here (single Access-Control-Allow-Origin value)
       strips client identity headers, injects trusted X-User-* context,
       issues short-lived HS256 assertion (iss=auth-gateway, aud=ai-service)
  -> ai-service POST /api/copilotkit
       ServiceAuthMiddleware verifies workload token from trusted sources
       (auth-gateway, ai-runtime legacy) before routing
       copilotkitEndpoint decodes {method,params,body} envelope
```

## Envelope contract (`internal/handler/copilotkit.go`)

Single endpoint implementing the CopilotKit single-route protocol:

- `{}` or `{"method":"info"}` →
  `{"agents":{"arda-assistant":{...}},"version":"arda-v1",...}`
- `{"method":"agent/run","body":{messages,...}}` → delegates to the same
  `runInputFlow` used by `/api/ai/agent`: SSE stream of AG-UI events
  (`RUN_STARTED` … text deltas … tool calls … approval proposal … `RUN_FINISHED`),
  conversation persistence, auto-title on first user message, HITL interrupt
  for confirm-kind tools.
- Invalid JSON body → problem `ai.invalid_copilotkit_envelope` (400); a warn
  log with a 200-byte preview is emitted for triage.
- Body cap: 1 MiB via `http.MaxBytesReader`.

Gateway policy entries relevant to AI (`apps/auth-gateway/configs/policy.yaml`):

| Policy id | Path | Methods |
| --- | --- | --- |
| `ai-agent-spike` | `/api/ai/agent/**` | POST |
| `ai-copilotkit-runtime` | `/api/copilotkit/**` | GET+POST+OPTIONS |
| `ai-conversations-read` | `/api/ai/conversations**` | GET |
| `ai-conversations-delete` | `/api/ai/conversations/**` | DELETE |
| `ai-approvals-write` | `/api/ai/approvals/**` | POST+OPTIONS |

All require permission `ai.assistant.use` (granted to ADMIN/SUPER_ADMIN roles
in IAM).

## Frontend wiring

`@workspace/ai` uses CopilotKit React v2 headless state with
`runtimeUrl = apiUrl() + "/api/copilotkit"` and `credentials: "include"`. The
gateway is the only CORS authority; the Go service keeps its built-in CORS
middleware disabled in production.

## Verification evidence (2026-08-26)

- Local: `go test ./internal/handler/ -run TestCopilotKitInfoEnvelope` passes.
- Public chain: authenticated `POST https://api.arda.io.vn/api/copilotkit` with
  `{"method":"info"}` → HTTP 200 with `agents.arda-assistant`; preflight
  returns exactly one `access-control-allow-origin: https://arda.io.vn`.
- Cluster: `ai-runtime` deployment pruned (NotFound); `ai-service` and
  `auth-gateway` rolled to images containing this code.

## Operational gotchas

- **PowerShell 5.1 mangles embedded quotes** when passing JSON inline to
  `curl.exe` (`--data-raw '{"a":1}'` arrives as `{a:1}`). Always pass request
  bodies via a temp file: `curl --data "@$env:TEMP\body.json"`. An "invalid
  envelope" 400 through the public URL with no matching server-side decode log
  is almost always client-side quote mangling, not a backend bug.
- Image updates flow GitHub Actions → GHCR digest → ArgoCD image updater →
  ArgoCD sync. A green CI build does not mean pods updated yet; compare the
  pinned `sha256:` digest on the Deployment before debugging behavior.
- `ServiceAuthMiddleware` wraps the entire mux, so any path on `ai-service`
  returns `ai.service_auth_required` (401) without a valid workload token —
  including unknown paths. Use that only as a liveness signal, not proof that
  a specific route exists.
