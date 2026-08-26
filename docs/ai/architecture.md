# AI architecture

## Goal

Provide a tenant-aware assistant for Arda users that can answer questions from
approved knowledge and read selected live Arda data, while preserving the
existing gateway, IAM, service ownership, and GitOps boundaries.

The first phase is an assistant and workflow co-pilot, not an autonomous
operator. The model proposes; Arda policy and domain services decide.

## Target shape

```text
React MFE / Arda shell
  -> auth-gateway: /api/copilotkit
        validates session, tenant, permission, recent-auth where required
  -> ai-service (Go + AG-UI, CopilotKit single-route envelope in Go)
        CopilotKit envelope endpoint (info + agent/run) — see
        go-native-copilotkit.md; the former Node ai-runtime adapter is retired
        AG-UI-compatible stream
        conversation/run state
        model adapter
        2 Meta-Tools (search & execute / Code Mode) & legacy tool registry
        embedded Goja JavaScript sandbox with arda.* SDK bindings
        RAG retrieval with ACL filtering
        AI operational audit
        -> domain service APIs/gRPC, never domain tables
        -> PostgreSQL database `ai`, public tables prefixed `ai_`
        -> approved model provider through egress policy
        -> NATS events for durable integration where needed
```

The existing `auth-gateway` remains the browser-facing BFF. It should route the
AI API only after the AI service exposes a health/readiness contract and the
gateway has an explicit policy entry. No generic authenticated pass-through is
allowed.

## Component responsibilities

### MFE

- Render chat, citations, tool status, and approval controls using Arda's design
  system.
- Send only user intent, conversation/run identifiers, and UI context allowed
  by the contract.
- Use the authenticated cookie/session already managed by Arda. Do not persist
  provider tokens or treat browser permissions as authoritative.
- Treat streamed tool calls and state as untrusted display data until the
  backend confirms the result.

### Auth gateway and IAM

- Authenticate the session and inject trusted actor, active tenant, roles,
  permissions, auth version, request ID, and recent-auth context.
- Strip client-supplied identity, tenant, role, permission, and auth headers.
- Enforce route-level permission and risk metadata before proxying.
- Keep IAM security audit as the source of truth for authentication and
  authorization events.

### CopilotKit boundary (implemented in Go)

- The browser-facing CopilotKit single-route endpoint lives inside `ai-service`
  (`/api/copilotkit`), not in a separate Node deployment. The former internal
  Node.js `ai-runtime` adapter is retired.
- The service verifies the short-lived `auth-gateway -> ai-service` workload
  assertion and the gateway-derived actor, tenant, and permission context.
- Browser cookies, authorization tokens, and arbitrary identity headers are
  never forwarded from the gateway beyond the trusted context headers it
  injects after stripping client-supplied values.
- UI-only `forwardedProps.ardaTool` hints are adapted into the Go request shape;
  Go remains the authority for tool allowlists, argument validation, and
  domain permissions.
- Conversation/run/tool persistence remains in the Go service and the
  Arda-owned `ai` database.

### AI service

- Resolve and re-check the actor/tenant context for every tool execution.
- Orchestrate the model, retrieval, tool calls, interrupts, and AG-UI events.
- Host the embedded Goja JavaScript sandbox for Code Mode (`execute`) and the
  SDK method catalog (`search`), providing dynamic discovery without context
  inflation.
- Bind `arda.*` SDK methods to domain HTTP/gRPC contracts with automatic tenant
  and permission injection.
- Persist conversation/run/tool state without storing raw credentials or hidden
  chain-of-thought.
- Call domain APIs through typed contracts and enforce tool-specific policy.
- Emit redacted operational events and metrics.

### Domain services

- Remain the owners of business data and invariants.
- Expose narrow read/command contracts for AI tools where approved.
- Re-authorize sensitive operations at the domain boundary; AI authorization is
  not a substitute for domain authorization.

### PostgreSQL and vector search

- The AI service owns its own database and migration history.
- `ai` tables do not join directly to IAM or domain tables. Store stable IDs and
  resolve display data through approved APIs.
- The `vector` extension is enabled as a database capability. It remains an
  optional implementation detail of the knowledge repository until the
  embedding model, dimension, index, and backup/restore plan are approved.

## Request lifecycle

1. Browser opens an authenticated CopilotKit request through the gateway.
2. Gateway validates session, route permission, tenant context, and risk, then
   signs a short-lived workload assertion with audience `ai-service`.
3. `ai-service` verifies the assertion via its trusted-source middleware and
   decodes the CopilotKit envelope (`info` or `agent/run`).
4. Go creates or resumes a run under the server-derived actor and tenant.
5. Retrieval applies tenant and document ACL filters before any content reaches
   the model.
6. The model may answer directly, invoke a named read tool, or use Code Mode:
   call `search` to discover SDK method signatures, then `execute` to run a
   sandboxed JavaScript script that composes multiple `arda.*` domain calls
   within a single turn. Tool policy, argument validation, tenant context, and
   permission checks apply whether the call originates from a direct tool or
   from inside the sandbox.
7. A high-risk mutation pauses for a server-enforced approval checkpoint.
   In Code Mode, a mutation SDK method called inside the sandbox yields an
   `ApprovalProposal` instead of executing the side effect.
8. AI service streams typed lifecycle/message/tool/approval events and commits
   the final run state.
9. AI operational audit and metrics record outcome, latency, policy decisions,
   and redaction-safe references.

## Non-goals for phase 1

- General autonomous task execution or background agents.
- Arbitrary SQL, shell commands, browser automation, or unrestricted HTTP tools.
- Cross-tenant search or a global knowledge index without explicit ACL semantics.
- Model-generated UI that can execute arbitrary frontend code.
- Replacing workflow-service, IAM, approval queues, or domain business rules.

## Architectural invariants

- No AI request can select a tenant by writing a trusted header or prompt value.
- No model output is treated as a permission decision.
- No side effect occurs only because a model emitted a tool call.
- Every tool call has a stable name, version, schema, actor, tenant, policy
  decision, timeout, and outcome.
- A code rollback must remain compatible with committed AI schema changes.
