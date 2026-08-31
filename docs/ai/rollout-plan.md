# AI rollout plan

## Gate 0 — architecture (current)

Deliver the docs in this directory, record the AG-UI protocol decision, and
review tool/permission names with backend, frontend, security, and operations
owners.

Exit criteria:

- no unresolved contradiction between service ownership, tenant context, tool
  policy, HITL, RAG ACLs, and database design;
- explicit approval of service-owned database `ai` and `public.ai_*` table
  ownership;
- an agreed first read-only use case and data classification;
- no production DB, secret, ingress, or workload mutation.

## Gate 1 — protocol and security spike (complete)

The deterministic Go endpoint existed at `arda-be/apps/ai-service`; the
gateway has a policy/upstream route for `/api/ai/**` served by the Olorin
panel. The AG-UI protocol replaced the earlier SSE spike: the shell panel now
streams through `useAgUiRuntime` + `HttpAgent` against `/api/ai/agent`.

Exit criteria met: compatibility tests pass, no trust-header bypass exists, and
the gateway injects a separate short-lived workload identity for the AI service.

## Gate 2 — persistence foundation (implemented; live rollout in progress)

The `ai` database and `arda_ai` role are provisioned additively in the real
CloudNativePG cluster. The service has additive Goose migrations for
conversations, messages, runs, tool executions, approvals, sources, chunks,
and feedback. The `vector` extension is now enabled, while the embedding
column/index remains disabled until its specific gates pass.

Before enabling user traffic, verify storage headroom and representative
read/write and retention behavior. Without `AI_ENABLE_AGENT` the endpoint
stays deterministic; with it, provider usage and tool records are written and
must be monitored.

## Gate 3 — first read-only vertical slice (implemented; live verification pending)

The first read-only SDK methods are `arda.crm.getCustomer` and
`arda.knowledge.search` (the `crm.customer.get` / `knowledge.search`
capabilities they map to). They are registered in the SDK catalog
(`internal/catalog`), tenant scoped, require separate IAM permissions, emit
AG-UI tool events, and persist redacted tool execution records. Knowledge uses
PostgreSQL full-text search over published sources; vector search and source
ingestion remain separate gates.

Success is measured by grounded/cited answers, zero ACL leakage, bounded
latency, and a clean failure path—not by autonomous breadth.

## Gate 4 — controlled HITL proposal (implemented; validation pending)

The service has a disabled-by-default, typed HITL boundary for the low-risk
`crm.customer.export.prepare` flow: a confirm-kind tool request during the
agent loop persists a server-side approval with a deterministic idempotency
key and pauses the run; an independent approver decides; the run owner resumes
through `/api/ai/approvals/{id}/execution`, which re-checks live permissions,
executes within the original tenant scope, and finishes the run. Failed
executions revert for retry. `prepare` still creates no export artifact.
Validate permission revocation, stale resource, duplicate resume, expiry,
reconnect, and audit behavior in a non-production environment before enabling
the flag.

No finance, IAM, MFA, permission, or irreversible mutation is included in this
gate.

## Gate 5 — CopilotKit runtime canary (**retired**)

The separate `ai-runtime` Node.js service originally planned for this gate was
evaluated and **retired before production deployment**. The CopilotKit
single-route envelope protocol (`/api/copilotkit`) was then served directly
from `ai-service` in Go, and was **fully replaced by the AG-UI protocol
(2026-08-31)** — see [go-native-copilotkit.md](go-native-copilotkit.md) for the
historical record. The `/api/copilotkit` endpoint, its policy route, and the
frontend `@copilotkit/react-core` dependency are removed; the assistant now
streams AG-UI events through `useAgUiRuntime` + `HttpAgent` on
`POST /api/ai/agent`.

The former two-hop design (gateway → Node ai-runtime → Go ai-service) was
replaced because the Node adapter added a deployable boundary and secret hop
without contributing any authorization decision. The manifest entry was pruned
from the ArgoCD kustomization and the deployment confirmed NotFound in the
cluster.

Security invariants (verified 2026-08-26, retained under AG-UI):

- `ai-service` verifies the workload assertion with audience `ai-service`;
- gateway issues a short-lived HS256 assertion with audience `ai-service`;
- gateway and ai-service reject missing, expired, wrong-audience, or forged
  workload assertions;
- the Go service remains the authority for tool policy and Arda persistence;
- frontend uses the assistant-ui AG-UI runtime with Arda-owned shadcn UI;
- rollback can disable the route/flag without deleting AI data.

## Gate 6 — production canary

Deploy separate, versioned backend/frontend/infra artifacts through Argo CD with
a feature flag, explicit resource limits, provider budget, and rollback plan.
Monitor gateway, AI, provider, database, and domain metrics independently.

Rollback code while preserving schema compatibility. Disable the feature flag or
route before considering any data/schema action. Never delete AI data as a
normal rollback step.

## Gate 7 — expansion & Code Mode evolution

Only after the canary meets security and quality gates may the team expand domains
and transition to the **2 Meta-Tools (Code Mode)** architecture:

1. **Embedded Sandbox POC:** Introduce the Goja ECMAScript sandbox engine within
   `ai-service` with strict resource bounds (CPU timeout, memory limits, anti-escape).
2. **2 Meta-Tools Integration:** Implement `search` (dynamic SDK discovery) and
   `execute` (sandboxed script running against `arda.*` SDK) as first-class tools.
3. **Domain Expansion via OpenAPI:** Generate TypeScript definitions and Go dispatchers
   from `contracts/openapi/` for CRM, HRM, Finance, and Workflow services, eliminating
   manual tool boilerplate.
4. **HITL in Code Sandbox:** Validate that mutation SDK calls inside scripts properly
   yield `ApprovalProposal` checkpoints and resume through the existing approval store.

## Stop conditions

Stop rollout and disable the feature if any of these occurs:

- cross-tenant or unauthorized source/tool data is observed;
- a tool executes without a valid server policy decision;
- audit correlation or redaction is broken;
- provider/model cost or latency exceeds the approved budget;
- migration lock/storage/backup health is unsafe;
- gateway or domain service health degrades beyond its existing SLO.

## Current implementation boundary

The current state adds the service-owned persistence foundation, the enabled
`vector` extension, bounded read tools plus one confirm-kind tool, the
model-driven agent loop over an OpenAI-compatible streaming provider behind
the `model.Provider` interface (env-configured, single source), incremental
SSE streaming, per-tenant rate limiting, graceful shutdown, owner-scoped
conversation APIs, the full HITL proposal/decision/execution path, and the
Olorin shell panel (`@workspace/ai`) with typed renderers, approval card with
resume, and thread history. It does not yet add multi-provider routing, vector
column/index, source ingestion, or any real mutation executor. The next gates
are an approved knowledge-source ingestion flow, provider evaluation/budget,
and the model-provider canary.
