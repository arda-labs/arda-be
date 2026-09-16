# ADR-004: Deadline budgets and the AI error contract

**Status:** Proposed (2026-09-16)
**Implementation status:** P0 landed (`e037b220`…`bd1f575f`, 2026-09-16):
bounded retrieval rewrite, sandbox error codes, audit status/code, NATS publish
fix. P1 landed (`ca6e9e19`…, 2026-09-16): `check-ai-budgets.mjs`,
`check-ai-errors.mjs`, the `docs.problemLookup` timeout, the retired
rag-service client, the legacy tenant-settings path, and the doc drift sweep.
P2 (segment latency) and P3 (async long-running tools) remain open —
[audit-2026-09.md](audit-2026-09.md) tracks them.
**Supersedes:** nothing
**Amends:** `performance-baseline.md` §4.3 (the 1.5 s sandbox rule becomes
normative and machine-checked), `audit-observability.md` (metric families gain
segment fields in a follow-up)
**Related:** `code-mode-design.md`, `sandbox-threat-model.md`,
`tool-contracts.md`, `knowledge-rag-design.md`, `multi-provider-design.md`

## Context

The 2026-09 knowledge-search outage was not a provider outage. Five timeout
layers existed with no relationship to each other:

| Layer | Value | Source |
|---|---|---|
| Code Mode sandbox wall clock | 3 000 ms | `internal/sandbox/engine.go:19` |
| Catalog entry (per SDK method) | 500–5 000 ms | `internal/catalog/*.go` |
| `execute` meta-tool | 4 000 ms | `internal/tools/meta_execute.go:35` |
| RAG query rewrite (LLM) | 10 000 ms, sequential | `internal/knowledge/service.go` (before `e037b220`) |
| Embedding / reranker HTTP clients | 30 000 ms / 10 000 ms | `internal/knowledge/embedder.go:30`, `reranker.go:31` |

`arda.knowledge.search` chained an LLM call, an embedding round-trip, and two
hybrid searches inside the 3 s sandbox. The inner call timed out against the
outer wall clock, the request context died, and production (fail-closed
embeddings) returned `ai.sandbox_timeout` for every search. `ai_rag_runs` had
zero rows: the capability never worked in production. Nothing in CI could have
caught it, because no invariant connects the layers.

The same investigation found the error contract equally implicit: sandbox
failures were persisted as `SUCCEEDED` with an empty `error_code`, and the
machine-readable code reached the model but not the audit trail.

## Decision

### 1. One deadline tree

The **caller's deadline** is the authoritative ceiling for any interactive SDK
call: the handler wraps each tool execution with that tool's timeout, and the
sandbox inherits it (`executionBudget`). `DefaultExecutionTimeout` (3 000 ms)
applies only to deadline-less callers; `MaxExecutionTimeout` (30 000 ms) caps
the rest. Every other timeout is either below the ceiling or a non-binding
backstop:

```text
agent run (AI_AGENT_RUN_TIMEOUT_SECONDS, 300 s)
└── SSE request
    └── meta-tool execution (`execute`, currently 10 000 ms — the caller)
        └── sandbox wall clock = caller deadline (default 3 000 ms, cap 30 000 ms)
            ├── SDK method ctx = min(catalog entry timeout, remaining wall clock)
            │   ├── mandatory stage  — fail-closed, may use the full remaining ctx
            │   └── optional stage   — own cap, must never block mandatory work
            └── provider/domain HTTP client timeouts — backstops only
```

Normative rules:

1. **R1.** No catalog entry may declare a timeout greater than the caller
   ceiling (`execute`). `docs.problemLookup` (was 5 000 ms) was lowered to
   2 000 ms, and `knowledge.search` (was 3 000 ms) was raised to 8 000 ms after
   production measured embedding round-trips above 3 s; the checker enforces
   the rule for every entry.
2. **R2.** A pipeline with optional stages must run them **concurrently** with
   the mandatory stage, under a constant cap strictly smaller than the ceiling
   (`knowledge.search`: `rewriteBudget` = 3 000 ms), and must **skip** optional
   work once the remaining deadline cannot cover the mandatory continuation
   plus a margin (`variantMinBudget` ≥ the measured primary-stage cost, floor
   1 000 ms). Deadline-bound callers give the optional stage a short grace
   period (`interactiveVariantWait` = 300 ms) before answering with the primary
   results; deadline-less callers wait for the full optional cap.
3. **R3.** Mandatory stages stay fail-closed: an embedding failure for the
   primary query is a retrieval failure, never a silent FTS-only result
   (production `AI_RAG_REQUIRE_EMBEDDING` behaviour is preserved).
4. **R4.** Client timeouts (30 s embedding, 10 s reranker) are backstops, never
   budgets. Code must rely on the request context, not the client timeout, to
   bound work.
5. **R5.** A long-running capability that cannot fit the ceiling (exports,
   reports, batch retrieval) must not raise the ceiling. It becomes an
   asynchronous job with a completion notification; the sandbox only starts it.

### 2. One error contract

Every failure that can reach a tool result, an audit row, or an API response
carries a machine-readable code with a fixed shape:

| Field | Meaning |
|---|---|
| `code` | `ai.<class>_<reason>` — stable, never a sentence |
| `class` | `sandbox`, `tool`, `approval`, `model`, `quota`, `run`, `persistence`, `boundary` |
| `retryable` | whether the caller/model may retry without changing input |
| `model_visible` | whether the model receives the code (structured tool errors) |
| `problem_page` | required for codes that can appear in an HTTP response; validated by `check-problem-catalog.mjs` |

Current inventory (representative; the full list is owned by
`scripts/check-ai-errors.mjs`):

| Code | Class | Retryable | Model-visible | Notes |
|---|---|---|---|---|
| `ai.sandbox_timeout` | sandbox | yes (rephrase) | yes | attached via `tools.SandboxError` |
| `ai.sandbox_busy` | sandbox | yes (backoff) | yes | concurrency caps |
| `ai.sandbox_budget_exceeded` | sandbox | no | yes | 50/20-call budgets |
| `ai.sandbox_output_too_large` | sandbox | no | yes | 64 KiB limit |
| `ai.tool_execution_failed` | tool | no | yes | generic mapping (`tools.ErrorCode`) |
| `ai.tool_invalid_arguments` | tool | no | yes | invalid model-supplied args |
| `ai.tool_forbidden` / `ai.tool_disabled` | tool | no | yes | authorization / governance |
| `ai.approval_unavailable` | approval | no | yes | HITL disabled or store missing |
| `ai.approval_stale` | approval | no | no | permission version changed |
| `ai.model_timeout` / `ai.model_unavailable` | model | yes | yes | provider failures |
| `ai.quota_exceeded` | quota | no | yes | tenant token budget |
| `ai.run_timeout` / `ai.run_cancelled` | run | n/a | no | terminal run states |

Normative rules:

1. **R6.** `tools.ErrorCode(err)` is the single mapping function for tool
   execution errors. Call sites do not invent codes.
2. **R7.** Sandbox failures attach `tools.SandboxError{Code, Err}`; the
   meta-tool sets `Result.ErrorCode` even when it returns structured error data
   to the model; the agent loop persists `FAILED` + `error_code`.
3. **R8.** Every new code must be added to the taxonomy table and (when
   API-facing) to `docs/problems/`. The checker fails when an API-facing code
   has no page and when a sandbox code is missing from §2; orphan pages (a page
   whose code no longer appears anywhere in the service) are reported for
   deliberate cleanup — retired surfaces are tracked in audit-2026-09 item A5.
4. **R9.** A code must not encode the same condition twice with different
   names. The historical `ai.execution_failed` / `ai.tool_execution_failed`
   overlap is resolved in favour of `ai.tool_execution_failed` for tool paths.

### 3. Enforcement (the part that makes this an invariant, not a doc)

- `scripts/check-ai-budgets.mjs` — parses `DefaultExecutionTimeout`, catalog
  entry timeouts, meta-tool timeouts, and the RAG budget constants; fails when
  R1/R2/R5 are violated.
- `scripts/check-ai-errors.mjs` — extracts `ai.*` codes emitted by the service
  (constants and literals), cross-checks the taxonomy and the problem catalog,
  and fails on missing pages or missing taxonomy rows.
- Both run in `.github/workflows/ai-invariants.yml` (node-only, so they report
  independently from `verify.yml`, which still runs `check-ai-catalog.mjs`).

## Alternatives considered

| Option | Why rejected |
|---|---|
| Raise the sandbox ceiling (e.g. to 10 s) | Widens the DoS surface, slows every interactive call, and does not fix the class: the next optional stage will exceed 10 s too |
| Permanently disable query rewrite | Removes the outage but keeps the bug class; also throws away a recall feature that fits fine when bounded |
| Per-method sandbox wall clock (engine change) | Larger engine surface (scheduling, fairness, per-script budgets); deferred until R5 async jobs prove insufficient |
| Adopt an external agent framework | The framework is not the problem; the contracts are. Migration would add a second source of truth |

## Consequences

Positive: knowledge search works within budget (verified in production on
2026-09-16), audit rows and metrics show the real failure class, and two new CI
gates make the next timeout-class regression impossible to merge silently.

Costs: two check scripts to maintain, one catalog entry timeout lowered, doc
updates, and a taxonomy table that must be kept current by PR.

Invariants to keep green:

1. Every catalog `Timeout` ≤ `sandbox.DefaultExecutionTimeout`.
2. Every optional stage cap < ceiling, with a named constant and a comment
   explaining the concurrency/skip rule.
3. Every `ai.*` code emitted by the service exists in the taxonomy and, when
   API-facing, in `docs/problems/`.
4. `ai_tool_executions` never records `SUCCEEDED` for a result that carries a
   failure code.

## Implementation plan

| Phase | Work | Gate |
|---|---|---|
| P0 (done, `e037b220`…`bd1f575f`) | Bounded rewrite; sandbox error codes; audit status/code; NATS publish fix | `go test ./...`; production `ai_rag_runs` rows appear |
| P1 (done, 2026-09-16) | Both check scripts + `ai-invariants.yml`; `docs.problemLookup` timeout; taxonomy table; doc drift sweep; retired rag-service client and legacy tenant-settings path removed; 25 orphan problem pages removed | `check-ai-budgets`/`check-ai-errors` green and red on a seeded violation; `go test ./...`; `bun run typecheck` (MFE) |
| P1b (done, 2026-09-17) | Sandbox wall clock follows the caller deadline (`executionBudget`, cap 30 s); `execute` 10 s; `knowledge.search` 8 s after measured embedding spikes; RAG-run `hit_ids` encoding fixed; `matchScore` reports cosine again | `check-ai-budgets`; new `executionBudget` unit test; `go test ./...` |
| P2 (follow-up) | Segment latency fields/spans per `audit-observability.md`; alert on publish failures | Grafana/dashboards show model TTFT vs retrieval vs domain latency |
| P3 (async) | Long-running tool pattern (R5): job + notification, sandbox only starts it | First real export/report tool uses the pattern |

## Non-goals

- No agent framework change, no sandbox rewrite.
- No durable run state machine in this ADR (tracked as A7 in the audit).
- No per-tenant budget policy beyond the existing quota reservation (revisit
  when a batch workload exists).
- No new requirement that every internal code has a public problem page —
  only API-facing codes do.

## Open questions

1. Should optional stages be allowed to *extend* the deadline when the
   mandatory work finished early (borrow unused budget), or stay hard-capped?
   Proposal: stay hard-capped; borrowing makes latency unpredictable.
2. Do we want the taxonomy table generated from code (single source) instead of
   maintained by hand? Proposal: yes in P2, once the checker exists.
3. For R5, is the existing notification-service inbox the delivery channel, or
   do we need an AI-specific job surface? Proposal: notification inbox.
