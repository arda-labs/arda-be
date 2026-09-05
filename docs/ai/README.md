# Arda AI phase

Status: the persistent read-only RAG slice, model-driven agent loop, versioned
AG-UI/SSE boundary, citation guard, quota reservation, cost ledger, and
readiness diagnostics are implemented in code; deployment verification is
environment-specific. The service enforces gateway identity and tenant scope,
persists runs/conversations, exposes structured citations, and keeps write tools
behind approval. Production readiness still requires a real model, published
knowledge content, gateway smoke tests, provider health/failover, and the
evaluation gate.

This directory is the source of truth for the first AI phase across `arda-be`,
`arda-mfe`, and `arda-infra`. The documents describe the committed baseline;
provider secrets, production corpus, and workload expansion remain deployment
gates.

## Master Specification

👉 **[architecture.md](architecture.md)** là tài liệu đặc tả chuẩn xác và cập nhật nhất cho toàn bộ hệ thống AI & RAG hiện tại của Arda.

## Decision summary

- Use a single, consolidated Go-native service boundary (`apps/ai-service`). It owns orchestration, AG-UI protocol, conversation state, and in-process RAG knowledge retrieval.
- Keep the browser boundary at `auth-gateway`; the browser never calls an LLM, domain database, vector store, or provider directly.
- Adopt AG-UI as the agent-to-UI contract: `ai-service` emits AG-UI SSE events, and the frontend runs the official assistant-ui AG-UI runtime (`useAgUiRuntime` + `HttpAgent`).
- All knowledge tables are managed in PostgreSQL `ai` database via Goose migrations in `apps/ai-service/migrations`.
- Historical/obsolete design spikes (Node.js runtime, CopilotKit, Python rag-service) are archived in `archive/`.

## Documents

1. [ARCHITECTURE.md](ARCHITECTURE.md) — master target components, RAG engine, and boundaries.
2. [agent-boundaries.md](agent-boundaries.md) — allowed and forbidden agent behavior.
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
12. [knowledge-ingestion.md](knowledge-ingestion.md) — knowledge source
    registration, chunking policy, review gate, embedding pipeline, versioning,
    and retention.
13. [evaluation-set.yaml](evaluation-set.yaml) — initial golden questions and
    negative cases used to gate RAG quality and tenant isolation.
14. [adr-001-rag-vertical-slice.md](adr-001-rag-vertical-slice.md) — the first
    read-only RAG acceptance scope and release gates.

The retrieval gate can be run with `go run ./cmd/ai-eval` from
`apps/ai-service`; it consumes `evaluation-set.yaml` and exits non-zero in
strict mode when expected evidence or no-answer cases fail.

Deferred designs stay in this directory until their phase starts: [multi-provider-design.md](multi-provider-design.md), [nats-events.md](nats-events.md), [enterprise-security-and-crypto.md](enterprise-security-and-crypto.md), [agent-evolution-roadmap.md](agent-evolution-roadmap.md), [catalog-scale-plan.md](catalog-scale-plan.md), [code-mode-design.md](code-mode-design.md), [sandbox-threat-model.md](sandbox-threat-model.md), [sdk-catalog-design.md](sdk-catalog-design.md), and [performance-baseline.md](performance-baseline.md). The former CopilotKit spike is retained under [archive/](archive/).

## Current repository evidence

- `arda-be` has Go services with service-owned PostgreSQL databases and Goose
  migrations. IAM owns users, tenants, permissions, MFA, and security audit.
- `arda-be/apps/ai-service` serves the AG-UI protocol on
  `/api/ai/agent` (events + `resume` entries for HITL interrupts), an optional
  model-driven agent loop (`AI_ENABLE_AGENT` + OpenAI-compatible provider),
  Goose migrations, tenant/actor-owned conversation persistence (list,
  messages, delete, auto-title), replay protection, production workload
  identity verification, and redacted read tools with knowledge citations.
  Internal HTTP tools (`iam.listUsers`, `crm.getCustomer`, `finance.getAccount`)
  are generated from `contracts/ai-internal/*.json` (`x-ai-tool`) via
  `tools/catalog-gen` with response-schema redaction; hand-written entries
  cover identity self-service, capability listing, knowledge search, and the
  local `crm.exportCustomer` approval stub. Confirm-kind tools create approval
  proposals instead of executing; the run owner resumes an approved proposal
  through an AG-UI interrupt response.
- `arda-mfe/apps/shell` ships the Olorin assistant as a docked, resizable side
  panel plus a full-screen workspace dialog (Ctrl/Cmd+J) built on
  `@workspace/ai` — the assistant-ui AG-UI runtime (`useAgUiRuntime` +
  `HttpAgent`), Arda-owned shadcn message UI with markdown rendering, thread
  switching/deletion backed by the conversations API, and an approval card
  that submits AG-UI interrupt responses.
- `arda-be` documents gateway-injected tenant/auth context and high-risk
  recent-auth/step-up requirements.
- `arda-mfe` is a Bun/Vite React MFE workspace with an existing cookie-based API
  client and shared auth package.
- `arda-infra` runs PostgreSQL through CloudNativePG PostgreSQL 18 and deploys
  application workloads through Argo CD/Kubernetes.

## Explicitly not done

There is still no production knowledge corpus until an owner publishes approved
sources. Retrieval now supports PostgreSQL full-text plus optional vector search
and an optional Cohere-compatible reranker; production automatically requires
an embedding provider (or `AI_RAG_REQUIRE_EMBEDDING=true` in CI/staging) so an
embedding failure fails closed instead of silently degrading to FTS-only. The vector
extension, embedding dimension constraint, and operational queue indexes are
managed by the AI migration, but production index/provider sizing still needs
an environment-specific rollout check.

`crm.customer.export.prepare` still creates no export artifact — it only
verifies scope; a real export executor must be designed with the owning domain
service. Multi-provider routing (cloud vs local model per tenant) is prepared
through the `model.Provider` interface but not implemented; provider
configuration is environment-based today (design in
[multi-provider-design.md](multi-provider-design.md)). NATS event publishing is
designed ([nats-events.md](nats-events.md)) but not yet wired into the service.
The service executes no other side effects.
