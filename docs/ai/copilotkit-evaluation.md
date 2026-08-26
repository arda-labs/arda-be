# CopilotKit and AG-UI evaluation

## Findings

AG-UI is a useful fit for Arda's streaming boundary. It is an event-based,
transport-agnostic protocol with lifecycle, message, tool-call, state, and
custom events. Its HTTP client supports an endpoint that accepts a run input
and streams events, including SSE. See the [AG-UI overview](https://docs.ag-ui.com/),
[architecture](https://docs.ag-ui.com/concepts/architecture), and
[event reference](https://docs.ag-ui.com/concepts/events).

CopilotKit is a React/Angular frontend stack built around AG-UI. It provides
chat, tool rendering, shared state, and human-in-the-loop UI. Its runtime also
provides routing, middleware, and authentication integration. The official
runtime documentation says direct agent connections leave authentication to the
application and are not the supported production path; see [Copilot Runtime](https://docs.copilotkit.ai/llamaindex/copilot-runtime)
and [authentication](https://docs.copilotkit.ai/auth).

## Fit against Arda

| Concern | Assessment | Decision |
| --- | --- | --- |
| React/Vite MFE | Good fit at UI layer | Evaluate CopilotKit React v2 components/hooks |
| Go microservices | No first-class Go runtime path identified in the current official integration set | Keep orchestration in Go; implement AG-UI-compatible endpoint |
| Existing cookie/BFF auth | Good if the gateway remains the enforcement point | Do not put provider tokens in the browser |
| Tenant and IAM policy | Requires Arda-owned server checks | CopilotKit cannot be the policy engine |
| HITL | Good presentation/resume model | Use CopilotKit UI only; enforce approval in Go and domain services |
| Existing MFE dependency rules | Additional shared package and bundle risk | Isolate in an AI feature/remote after a bundle check |
| Runtime operations | A Node runtime adds a new deployable and secret boundary | Accept for CopilotKit; keep it internal and stateless |

## Decision

### Adopt now (original decision)

- Adopt AG-UI-compatible event semantics as the stable agent/UI boundary.
- Keep the versioned Go AG-UI endpoint behind the runtime and `auth-gateway`.
- ~~Deploy a separate Node.js `ai-runtime` with CopilotKit Runtime and `HttpAgent`
  to the Go endpoint.~~ **Superseded — see updated decision below.**
- Use CopilotKit React v2 headless state with Arda-owned shadcn UI; do not make
  the default CopilotKit chat component a requirement.
- Define Arda-specific custom events only for citations, approval cards, and
  policy/status data that do not fit standard events.

### Updated decision (2026-08-26) — ai-runtime retired, Go-native active

The separate `ai-runtime` Node.js deployment was **retired before reaching
production**. Evaluation found it added a deployable, a secret boundary, and a
second workload-token hop without contributing any authorization decision.

Current production state:

- `ai-service` (Go) serves the CopilotKit single-route envelope protocol
  (`/api/copilotkit`) directly; see [go-native-copilotkit.md](go-native-copilotkit.md).
- The gateway signs a short-lived HS256 assertion with audience `ai-service`
  (not `ai-runtime`); `ServiceAuthMiddleware` verifies it before routing.
- `arda-be/apps/ai-runtime` source is retained for reference only; nothing deploys it.
- `arda-infra/k8s/apps/ai-runtime.yaml` was deleted from the kustomization;
  ArgoCD confirmed the deployment as NotFound in the cluster.

### Evaluate after the first spike

- CopilotKit React v2 `useHumanInTheLoop`, threads, inspector, and richer
  generated UI against the Go-native endpoint.
- Whether each new CopilotKit feature preserves Arda's server-side policy and
  persistence boundaries.

### Do not adopt in phase 1

- CopilotKit Runtime as the production authorization or tenant boundary.
- Direct browser-to-model or browser-to-agent connections.
- CopilotKit generative UI that can construct unrestricted Arda actions.
- Framework-specific state persistence that bypasses Arda's `ai` records.

## Compatibility spike acceptance criteria

The spike is successful only if all of these pass:

1. An authenticated MFE request streams `RUN_STARTED`, message events, and
   `RUN_FINISHED` through the gateway.
2. Cookie/session auth and active tenant are preserved without browser-supplied
   trust headers.
3. A denied permission produces a stable 403/problem response and no model/tool
   invocation.
4. A read-only tool call renders status and a redacted result.
5. An approval event pauses and resumes a run without losing the run ID.
6. Disconnect, timeout, duplicate resume, and revoked-session cases fail closed.
7. `bun run typecheck`, Node runtime build, Go tests, contract checks, and
   `git diff --check` pass.

## Versioning rule

Pin AG-UI/CopilotKit package versions. Treat protocol event shape, custom event
names, and tool schemas as versioned contracts. Upgrade in a compatibility PR,
not as an incidental dependency refresh.
