# Arda AI phase

Status: the persistent read-only vertical slice is implemented and verified
end-to-end on the real K3s cluster. The model-driven agent loop
(OpenAI-compatible streaming provider behind a `model.Provider` interface),
server-enforced approval proposals for `confirm`-kind tools, owner-triggered
approval execution, owner-scoped conversation APIs with delete and auto-title,
and the Go-native CopilotKit envelope endpoint are deployed; an authenticated
browser request through `https://api.arda.io.vn/api/copilotkit` returns the
agent descriptor (HTTP 200). Remaining: production knowledge content, vector
retrieval, and any real mutation beyond `prepare`.

This directory is the source of truth for the first AI phase across `arda-be`,
`arda-mfe`, and `arda-infra`. The documents precede model-provider secrets,
vector schema/index changes, and production workload expansion.

## Decision summary

- Use a new Arda-owned AI service boundary. It owns orchestration, tool policy,
  knowledge retrieval, conversation state, and AI operational records.
- Keep the browser boundary at `auth-gateway`; the browser never calls an LLM,
  domain database, vector store, or provider directly.
- Adopt AG-UI-compatible streaming as the agent-to-UI contract.
- Serve the CopilotKit single-route envelope protocol directly from the Go
  `ai-service` (`/api/copilotkit`). The former separate internal Node.js
  `ai-runtime` adapter is **retired** (manifest pruned from ArgoCD); see
  [go-native-copilotkit.md](go-native-copilotkit.md) for the contract, gateway
  audience (`ai-service`), and verification evidence.
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
12. [go-native-copilotkit.md](go-native-copilotkit.md) — current CopilotKit
    boundary in Go, gateway policy routes, verification evidence, and ops
    gotchas.
13. [code-mode-design.md](code-mode-design.md) — 2 Meta-Tools architecture
    (`search` & `execute` / Code Mode) using embedded Goja sandbox for scalable
    cross-domain operations.
14. [sandbox-threat-model.md](sandbox-threat-model.md) — attack scenarios,
    mitigations, and security invariants for the Goja sandbox runtime.
15. [sdk-catalog-design.md](sdk-catalog-design.md) — SDK catalog build pipeline,
    BM25 search index, dispatcher registry, and CI consistency checks for the
    `search` meta-tool.
16. [performance-baseline.md](performance-baseline.md) — token cost model,
    latency budgets, provider budget controls, and canary success criteria for
    Code Mode rollout.
17. [multi-provider-design.md](multi-provider-design.md) — multi-provider and
    model routing design for tenant-plan-based and feature-flag-based provider
    selection.
18. [knowledge-ingestion.md](knowledge-ingestion.md) — knowledge source
    registration, chunking policy, review gate, embedding pipeline, versioning,
    and retention.
19. [nats-events.md](nats-events.md) — NATS JetStream event contracts for run,
    approval, knowledge, and audit events consumed by notification, IAM, and
    platform services.
20. [enterprise-security-and-crypto.md](enterprise-security-and-crypto.md) —
    Enterprise security tiers, AES-256-GCM envelope encryption, Blind Indexing
    search, KeyProvider (KMS/Vault) integration, and SSRF egress filtering.

## Current repository evidence

- `arda-be` has Go services with service-owned PostgreSQL databases and Goose
  migrations. IAM owns users, tenants, permissions, MFA, and security audit.
- `arda-be/apps/ai-service` contains the AG-UI boundary with the Go-native
  CopilotKit envelope endpoint (`/api/copilotkit`), an optional model-driven
  agent loop (`AI_ENABLE_AGENT` + OpenAI-compatible provider), Goose
  migrations, tenant/actor-owned conversation persistence (list, messages,
  delete, auto-title), replay protection, production workload identity
  verification, allowlisted read-only `crm.customer.get`, `knowledge.search`,
  and `crm.customer.export.prepare` tools with redacted output and knowledge
  citations. Confirm-kind tools create approval proposals instead of executing;
  the run owner resumes an approved proposal through
  `/api/ai/approvals/{id}/execution`.
- `arda-mfe/apps/shell` ships the Olorin assistant as a docked, resizable side
  panel plus a full-screen workspace dialog (Ctrl/Cmd+J) built on
  `@workspace/ai` — CopilotKit headless AG-UI state, Arda-owned shadcn message
  UI with markdown rendering, thread switching/deletion backed by the
  conversations API, and an approval card with resume.
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
environment-based today (design in [multi-provider-design.md](multi-provider-design.md)).
NATS event publishing is designed ([nats-events.md](nats-events.md)) but not yet
wired into the service. The service executes no other side effects.
