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
  -> auth-gateway: /api/ai/*
       validates session, tenant, permission, recent-auth where required
  -> ai-service (Go)
       AG-UI-compatible stream
       conversation/run state
       model adapter
       allowlisted tool registry
       RAG retrieval with ACL filtering
       AI operational audit
       -> domain service APIs/gRPC, never domain tables
       -> PostgreSQL database `ai`, schema `ai`
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

### AI service

- Resolve and re-check the actor/tenant context for every tool execution.
- Orchestrate the model, retrieval, tool calls, interrupts, and AG-UI events.
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
- `pgvector` is an optional implementation detail of the knowledge repository,
  enabled only after the embedding model, dimension, index, and backup/restore
  plan are approved.

## Request lifecycle

1. Browser opens an authenticated AI request through the gateway.
2. Gateway validates session, route permission, tenant context, and risk.
3. AI service creates or resumes a run under the server-derived actor and tenant.
4. Retrieval applies tenant and document ACL filters before any content reaches
   the model.
5. The model may answer or request an allowlisted tool. Tool policy validates
   arguments and calls the owning service.
6. A high-risk mutation pauses for a server-enforced approval checkpoint.
7. AI service streams typed lifecycle/message/tool/approval events and commits
   the final run state.
8. AI operational audit and metrics record outcome, latency, policy decisions,
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
