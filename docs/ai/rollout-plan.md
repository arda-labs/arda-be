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

## Gate 1 — protocol and security spike (in progress)

The initial deterministic Go endpoint now exists at
`arda-be/apps/ai-service` with no model credential, database, or tool. The
gateway has a policy/upstream route and the shell has a local-only
`/ai-protocol-spike` page enabled with `VITE_AI_PROTOCOL_SPIKE=true`. The
remaining Gate 1 work is to run the stack together and exercise reconnect,
cancellation, and error envelopes.

Exit criteria: compatibility tests pass, no trust-header bypass exists, and
stream metrics/log redaction are visible.

## Gate 2 — persistence foundation

Provision the `ai` database/role through the existing GitOps/database process,
then add additive Goose migrations for conversations, messages, runs, tool
executions, approvals, sources, chunks, and feedback as approved. Do not enable
`pgvector` until its specific gates pass.

Before production apply, capture a database backup/reference, verify storage
headroom, apply on an isolated restore, and test representative read/write and
retention behavior.

## Gate 3 — first read-only vertical slice

Implement one assistant use case with one knowledge source class and at most a
few read-only tools. Route it through `auth-gateway`, enforce IAM permissions,
scope retrieval and tool reads by tenant, and ship citations plus audit/metrics.

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

The documentation pass and Gate 1 protocol spike change no database, Kubernetes
resource, secret, provider account, SSH host, or production deployment. The
next implementation action is the gateway/MFE compatibility part of Gate 1.
