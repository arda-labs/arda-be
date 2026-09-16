# ADR-005: Agent run lifecycle and result durability

**Status:** Proposed (2026-09-16)
**Implementation status:** not implemented — this ADR proposes the direction and
must be accepted before code.
**Supersedes:** nothing
**Amends:** nothing
**Related:** `audit-2026-09.md` item A7, `code-mode-design.md`,
`human-in-the-loop.md`, `agent-boundaries.md`, `database-design.md`

## Context

A run is currently one HTTP/SSE request: `POST /api/ai/agent` loads history,
runs the model/tool loop (up to `AI_AGENT_MAX_STEPS`), streams events, and
persists the terminal state. The only resumable interruption is HITL approval
(`internal/handler/resume.go`); everything else is lost if the process dies or
the client disconnects mid-run.

Two consequences observed in production and in the 2026-09 audit:

1. **No crash recovery.** A pod restart or eviction during a run surfaces as
   `FAILED`/`CANCELLED`; the model calls already paid for are lost and the user
   must retry from scratch.
2. **Request-scoped results.** The sandbox `ResultStore`
   (`internal/sandbox/resultstore.go`) is in-memory, 15-minute TTL, namespace =
   request ID. A `resultId` that appears earlier in the conversation can never
   be fetched in a later run; with two replicas there is no shared state at
   all. The `readResult` contract the system prompt advertises is therefore
   same-run only.

The stack already has most of the persistence needed: `ai_runs`, `ai_messages`,
`ai_tool_executions`, `ai_conversations`, plus Zeebe for cross-service sagas.

## Decision (proposed)

### 1. Runs become step-durable in the AI database (near term)

Persist the loop as ordered steps before executing them, in `ai_run_steps`:

| Column | Purpose |
|---|---|
| `run_id`, `step_index` | identity, ordering |
| `kind` | `model_turn`, `tool_call`, `approval_wait` |
| `status` | `pending`, `running`, `succeeded`, `failed` |
| `idempotency_key` | hash(run, step, tool, arguments) for replay safety |
| `request` / `result` (redacted, encrypted where needed) | replay and audit |
| `started_at`, `finished_at`, `error_code` | observability |

Resume after a crash replays from the last `succeeded` step, reusing stored
results and failing closed when the step's idempotency guard cannot be
reconstructed. Tool calls that mutate (confirm-kind) remain behind HITL and are
never replayed implicitly.

### 2. Result store moves to a shared store keyed by thread (near term)

Replace the in-process `ResultStore` with Redis (already wired for rate
limiting) or a Postgres table keyed by `(tenant_id, thread_id, result_id)`, with
the same size/TTL caps. This makes `readResult` work across turns and replicas,
matching the "filesystem as context" contract the prompt advertises. Until it
lands, the tool description must state the same-run limitation explicitly.

### 3. Zeebe stays a cross-service tool, not the agent loop (decision)

Zeebe is already the right tool for *business* sagas (loan, finance). Driving
the agent loop through Zeebe would couple the model/tool iteration to BPMN
modelling and add a hop per turn for no recovery benefit the step table cannot
provide. Revisit only if agent steps must participate in multi-service
transactions.

### 4. No new durable-execution infrastructure (non-goal)

Temporal/Restate/DBOS-class systems solve this class well, but the AI service is
a single Go process with one database; adopting another runtime for one
workflow shape is cost without a proven need. The step table is ~two migrations
and one dispatcher change.

## Alternatives considered

| Option | Why rejected (for now) |
|---|---|
| Keep request-scoped runs and document the limits | Cheapest, but leaves the crash-recovery and cross-replica gaps the audit flagged; acceptable only if the product explicitly accepts them |
| Zeebe-driven agent loop | Over-couples model iteration to BPMN, adds latency per turn, duplicates run state in two systems |
| Temporal/Restate/DBOS | New runtime to operate for one service; revisit if the loop grows multi-service side effects |
| Client-side resume (SSE Last-Event-ID replay only) | Events can be replayed, but model/tool side effects cannot be undone; insufficient alone |

## Consequences

Positive: crash/eviction recovery with cost accounting intact, cross-turn
`readResult`, honest cancellation semantics, and a single audit story per run.

Costs: two migrations, a step dispatcher in the agent loop, idempotency keys for
every tool call, retention policy for stored step payloads, and tighter care
around confirm-kind replays.

Invariants to keep green:

1. A resumed run never re-executes a `succeeded` step; confirm-kind steps are
   never replayed without an explicit approval row in `APPROVED` state.
2. Step payloads obey the existing redaction/encryption rules
   (`enterprise-security-and-crypto.md`).
3. `ai_tool_executions` remains the audit source for tool outcomes; the step
   row references it, it does not replace it.

## Open questions

1. Retention for step payloads: 30 days like conversations, or shorter?
2. Does SSE reconnect become a supported resume path for users (same `run_id`),
   or is resume internal-only after a crash?
3. Should `readResult` capabilities grow beyond the current 64 KiB preview
   limit once results are shareable across turns?
