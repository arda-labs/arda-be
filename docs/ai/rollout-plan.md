# AI rollout plan

## Gate 0 — architecture (current)

Deliver the docs in this directory, record the CopilotKit/AG-UI decision, and
review tool/permission names with backend, frontend, security, and operations
owners.

Exit criteria:

- no unresolved contradiction between service ownership, tenant context, tool
  policy, HITL, RAG ACLs, and database design;
- explicit approval of `ai` database/schema ownership;
- an agreed first read-only use case and data classification;
- no production DB, secret, ingress, or workload mutation.

## Gate 1 — protocol and security spike (complete)

The initial deterministic Go endpoint now exists at
`arda-be/apps/ai-service` with no model credential, database, or tool. The
gateway has a policy/upstream route and the shell has a local-only
`/ai-protocol-spike` page enabled with `VITE_AI_PROTOCOL_SPIKE=true`. The
remaining Gate 1 work is to run the stack together and exercise reconnect,
cancellation, and error envelopes.

Exit criteria met: compatibility tests pass, no trust-header bypass exists, and
the gateway injects a separate short-lived workload identity for the AI service.

## Gate 2 — persistence foundation (implemented; live rollout in progress)

The `ai` database and `arda_ai` role are provisioned additively in the real
CloudNativePG cluster. The service has additive Goose migrations for
conversations, messages, runs, tool executions, approvals, sources, chunks,
and feedback. `pgvector` remains disabled until its specific gates pass.

Before enabling user traffic, verify storage headroom and representative
read/write and retention behavior. The first live rollout keeps the endpoint
deterministic and records no provider or tool data.

## Gate 3 — first read-only vertical slice (implemented; live verification pending)

The first read-only tools are `crm.customer.get` and `knowledge.search`. They
are server-registered, tenant scoped, require separate IAM permissions, emit
AG-UI tool events, and persist redacted tool execution records. Knowledge uses
PostgreSQL full-text search over published sources; vector search and source
ingestion remain separate gates.

Success is measured by grounded/cited answers, zero ACL leakage, bounded
latency, and a clean failure path—not by autonomous breadth.

## Gate 4 — controlled HITL proposal

Add one low-risk proposal or draft flow with server-side approval and idempotency
in a non-production environment. Validate permission revocation, stale resource,
duplicate resume, expiry, reconnect, and audit behavior.

No finance, IAM, MFA, permission, or irreversible mutation is included in this
gate.

## Gate 5 — production canary

Deploy separate, versioned backend/frontend/infra artifacts through Argo CD with
a feature flag, explicit resource limits, provider budget, and rollback plan.
Monitor gateway, AI, provider, database, and domain metrics independently.

Rollback code while preserving schema compatibility. Disable the feature flag or
route before considering any data/schema action. Never delete AI data as a
normal rollback step.

## Gate 6 — expansion

Only after the canary meets security and quality gates may the team add more
domains, knowledge classes, or mutation tools. Each addition gets its own tool
contract, permission, risk classification, evaluation set, and rollback story.

## Stop conditions

Stop rollout and disable the feature if any of these occurs:

- cross-tenant or unauthorized source/tool data is observed;
- a tool executes without a valid server policy decision;
- audit correlation or redaction is broken;
- provider/model cost or latency exceeds the approved budget;
- migration lock/storage/backup health is unsafe;
- gateway or domain service health degrades beyond its existing SLO.

## Current implementation boundary

The current change adds the service-owned persistence foundation, two bounded
read-only tools, the feature-flagged frontend route, and GitOps wiring. It does
not add a model provider, `pgvector`, source ingestion, or mutation tool. The
next gate is live canary verification, followed by an explicitly approved
knowledge-source ingestion flow and only then a non-production HITL proposal.
