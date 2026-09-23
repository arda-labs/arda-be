# Arda AI phase

Status: the persistent read-only RAG slice, model-driven agent loop, versioned
AG-UI/SSE boundary, citation guard, quota reservation, cost ledger, and
readiness diagnostics are implemented in code; deployment verification is
environment-specific. The service enforces gateway identity and tenant scope,
persists runs/conversations, exposes structured citations, and keeps write tools
behind approval. A tenant-configurable Jev decision layer routes each new run to
server-owned guidance (fail-open, no capability grant) and has offline routing
and answer-judge evaluation tooling; the judge stays report-only. Production
readiness still requires a real model, published knowledge content, gateway
smoke tests, provider health/failover, and the evaluation gate.

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
- Tool governance follows [adr-003-tool-governance-and-sources.md](adr-003-tool-governance-and-sources.md): platform-level enable/disable (governance, not authorization), a source-driven catalog UI in `/ai/tools`, and MCP kept as an integration boundary (exposure adapter deferred until a real client; external MCP consumption needs its own ADR).

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
15. [adr-002-tool-authorization.md](adr-002-tool-authorization.md) —
    permission-first tool execution, the `enabled` registry flag, and the
    HIGH-risk tier.
16. [adr-003-tool-governance-and-sources.md](adr-003-tool-governance-and-sources.md)
    — catalog enable/disable governance, source-driven catalog UI, and the MCP
    exposure/consumption boundary.
17. [adr-004-budget-and-error-contract.md](adr-004-budget-and-error-contract.md)
    — the deadline tree for tool execution, the machine-readable AI error
    contract, and the CI gates that enforce both.
18. [adr-005-run-lifecycle.md](adr-005-run-lifecycle.md) — proposed step-durable
    run lifecycle and shared result store (audit item A7).
19. [decision-models.md](decision-models.md) — the System One (Jev) decision
    layer: tenant settings, routing runtime, the routing golden set, and the
    routing OFF vs ON results.
20. [judge-calibration.md](judge-calibration.md) — the report-only answer judge
    (grounded, answers_question, correct_abstention) and its calibration
    against human labels.

The September 2026 stack audit — findings with file/line and production
evidence, severity, and the Phase A cleanup order — is
[audit-2026-09.md](audit-2026-09.md).

The evaluation gates run with `go run ./cmd/ai-eval` from `apps/ai-service`:
`-mode=retrieval` (golden RAG questions and no-answer cases), `-mode=routing`
(offline Jev routing set: accuracy, macro F1, critical false positives,
confidence sweep), `-mode=answer` (full agent answers plus the optional
report-only Jev judge) and `-mode=judge-calibrate` (judge against human
labels). Strict exits stay off by default; see
[decision-models.md](decision-models.md) and
[judge-calibration.md](judge-calibration.md).

Deferred designs stay in this directory until their phase starts: [multi-provider-design.md](multi-provider-design.md), [nats-events.md](nats-events.md), [enterprise-security-and-crypto.md](enterprise-security-and-crypto.md), [agent-evolution-roadmap.md](agent-evolution-roadmap.md), [catalog-scale-plan.md](catalog-scale-plan.md), [code-mode-design.md](code-mode-design.md), [sandbox-threat-model.md](sandbox-threat-model.md), [sdk-catalog-design.md](sdk-catalog-design.md), and [performance-baseline.md](performance-baseline.md). The former CopilotKit spike is retained under [archive/](archive/).

## Current repository evidence

- `arda-be` has Go services with service-owned PostgreSQL databases and Goose
  migrations. IAM owns users, tenants, permissions, MFA, and security audit.
- `arda-be/apps/ai-service` serves the AG-UI protocol on
  `/api/ai/agent` (events + `resume` entries for HITL interrupts), a
  model-driven agent loop (tenant-owned provider profiles configured in the AI
  Settings UI; the deployment supplies only a shared gateway token and base-URL
  allowlist), Goose migrations, tenant/actor-owned conversation persistence
  (list, messages, delete, auto-title), replay protection, production workload
  identity verification, and redacted read tools with knowledge citations.
  Tenant-owned decision routing (`ai_decision_settings`, Jev key `enc:v1`) adds
  reviewed server-owned guidance per new run and fails open to the normal path.
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
service. Multi-provider routing (cloud vs local model per tenant) is
implemented through provider profiles and the `model.Provider` pool (design in
[multi-provider-design.md](multi-provider-design.md)); the applied profile +
model is the single source of truth, and the legacy `ai_tenant_settings` path
was removed with audit-2026-09 item A5. NATS event publishing is wired
([nats-events.md](nats-events.md)); the JetStream publish bug that silently
buffered every event was fixed in 2026-09. The service executes no other side
effects.
