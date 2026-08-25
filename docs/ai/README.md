# Arda AI phase

Status: architecture and design review complete; Gate 1 protocol spike is in
progress.

This directory is the source of truth for the first AI phase across `arda-be`,
`arda-mfe`, and `arda-infra`. The documents are intentionally written before
any AI database migration, `pgvector` enablement, model-provider secret, or
production workload change.

## Decision summary

- Use a new Arda-owned AI service boundary. It owns orchestration, tool policy,
  knowledge retrieval, conversation state, and AI operational records.
- Keep the browser boundary at `auth-gateway`; the browser never calls an LLM,
  domain database, vector store, or provider directly.
- Adopt AG-UI-compatible streaming as the agent-to-UI contract.
- Do not deploy CopilotKit Runtime in the first production slice. CopilotKit
  React components/hooks may be evaluated against the AG-UI endpoint after the
  contract and authentication tests pass.
- Start with read-only, tenant-scoped tools and cited knowledge answers.
  Mutations require server-side authorization, idempotency, and human approval.
- Use a service-owned PostgreSQL database named `ai` and an `ai` schema inside
  it, following Arda's service-owned database rule. Enable `pgvector` only
  after the database design and embedding model/dimension are approved.
- Reuse IAM for identity and authorization decisions. AI records are not a
  replacement for IAM security audit records.

## Documents

1. [architecture.md](architecture.md) — target components and boundaries.
2. [copilotkit-evaluation.md](copilotkit-evaluation.md) — CopilotKit and AG-UI
   fit, alternatives, and adoption decision.
3. [agent-boundaries.md](agent-boundaries.md) — allowed and forbidden agent
   behavior.
4. [human-in-the-loop.md](human-in-the-loop.md) — approval and interrupt rules.
5. [tool-contracts.md](tool-contracts.md) — typed tool contract and execution
   lifecycle.
6. [knowledge-rag-design.md](knowledge-rag-design.md) — ingestion, retrieval,
   access control, citations, and prompt-injection handling.
7. [conversation-memory.md](conversation-memory.md) — conversation and memory
   policy, retention, and redaction.
8. [security-permissions.md](security-permissions.md) — identity, tenant,
   permissions, secrets, and failure behavior.
9. [audit-observability.md](audit-observability.md) — audit events, traces,
   metrics, and redaction.
10. [database-design.md](database-design.md) — proposed schema and migration
    gates. No SQL migration is authorized by this document alone.
11. [rollout-plan.md](rollout-plan.md) — staged implementation and rollback.

## Current repository evidence

- `arda-be` has Go services with service-owned PostgreSQL databases and Goose
  migrations. IAM owns users, tenants, permissions, MFA, and security audit.
- `arda-be/apps/ai-service` now contains a deterministic, no-persistence AG-UI
  protocol spike used only for contract tests.
- `arda-mfe/apps/shell` has a local-only `/ai-protocol-spike` page, enabled only
  with `VITE_AI_PROTOCOL_SPIKE=true`, and a dev proxy for `/api/ai`.
- `arda-be` documents gateway-injected tenant/auth context and high-risk
  recent-auth/step-up requirements.
- `arda-mfe` is a Bun/Vite React MFE workspace with an existing cookie-based API
  client and shared auth package.
- `arda-infra` runs PostgreSQL through CloudNativePG PostgreSQL 18 and deploys
  application workloads through Argo CD/Kubernetes.

## Explicitly not done

At the time of this review there is no AI migration, `ai` database, `pgvector`
extension, model credential, AI ingress, or AI frontend route in the
repositories. The protocol spike is not a production AI service and must not be
deployed as one. The next phase must add persistence and integrations
additively, verifying each gate before moving on.
