# Arda AI phase

Status: the persistent read-only vertical slice is implemented and rolling out
to the real K3s cluster. The model-driven agent loop (OpenAI-compatible
streaming provider behind a `model.Provider` interface), server-enforced
approval proposals for `confirm`-kind tools, owner-triggered approval
execution, and owner-scoped conversation APIs are implemented and tested.
Remaining: production knowledge content, vector retrieval, and any real
mutation beyond `prepare`.

This directory is the source of truth for the first AI phase across `arda-be`,
`arda-mfe`, and `arda-infra`. The documents precede model-provider secrets,
vector schema/index changes, and production workload expansion.

## Decision summary

- Use a new Arda-owned AI service boundary. It owns orchestration, tool policy,
  knowledge retrieval, conversation state, and AI operational records.
- Keep the browser boundary at `auth-gateway`; the browser never calls an LLM,
  domain database, vector store, or provider directly.
- Adopt AG-UI-compatible streaming as the agent-to-UI contract.
- Deploy CopilotKit Runtime as a separate internal Node.js `ai-runtime` service
  in the first CopilotKit production slice. It is reached only through
  `auth-gateway`; the browser never connects to the runtime or Go service
  directly. The runtime is an AG-UI adapter, not Arda's authorization boundary.
- Start with read-only, tenant-scoped tools and cited knowledge answers.
  Mutations require server-side authorization, idempotency, and human approval.
- Use a service-owned PostgreSQL database named `ai`. Its application tables
  live in `public` with an `ai_` prefix, matching existing Arda services;
  `public.goose_db_version` remains migration metadata only. The `vector`
  extension is enabled, but embedding schema/index work waits for an approved
  model and dimension.
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
- `arda-be/apps/ai-service` contains the AG-UI boundary with an optional
  model-driven agent loop (`AI_ENABLE_AGENT` + OpenAI-compatible provider),
  Goose migrations, tenant/actor-owned conversation persistence, replay
  protection, production workload identity verification, allowlisted read-only
  `crm.customer.get`, `knowledge.search`, and `crm.customer.export.prepare`
  tools with redacted output and knowledge citations. Confirm-kind tools
  create approval proposals instead of executing; the run owner resumes an
  approved proposal through `/api/ai/approvals/{id}/execution`.
- `arda-mfe/apps/shell` ships the Olorin assistant panel (`@workspace/ai`,
  Ctrl/Cmd+J) using CopilotKit's headless AG-UI state with Arda's shadcn
  `Message`/`MessageScroller` UI, typed tool renderers, an approval card with
  resume, and conversation history backed by the new conversations API.
- `arda-be` documents gateway-injected tenant/auth context and high-risk
  recent-auth/step-up requirements.
- `arda-mfe` is a Bun/Vite React MFE workspace with an existing cookie-based API
  client and shared auth package.
- `arda-infra` runs PostgreSQL through CloudNativePG PostgreSQL 18 and deploys
  application workloads through Argo CD/Kubernetes.

## Explicitly not done

The rollout has the `vector` extension but no vector column/index, and no
production knowledge content until an owner publishes approved sources.
Knowledge retrieval uses PostgreSQL full-text search. `crm.customer.export.prepare`
still creates no export artifact — it only verifies scope; a real export
executor must be designed with the owning domain service. Multi-provider
routing (cloud vs local model per tenant) is prepared through the
`model.Provider` interface but not implemented; provider configuration is
environment-based today. The service executes no other side effects.
