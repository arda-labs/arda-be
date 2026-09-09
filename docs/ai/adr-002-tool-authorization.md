# ADR-002: Permission-first tool execution for AI agents

**Status:** Accepted (adjusted from the owner's proposal, 2026-09-09)
**Supersedes:** nothing
**Related:** adr-001-rag-vertical-slice.md, security-permissions.md, human-in-the-loop.md, tool-contracts.md

## Decision

Tool execution for AI agents is **permission-first**. Human approval (HITL) is
an additional control for medium/high-risk actions, never the default gate for
every write or export. This ratifies the architecture already implemented in
`apps/ai-service` and extends it with an explicit `enabled` registry flag and a
formal HIGH-risk tier.

```text
User chat request
  → model proposes a tool call          (LLM never grants authority)
  → ai-service Registry.Resolve         (tool exists, version match)
  → permission check                    (RequiredPermissions ⊆ gateway-injected X-Permissions)
  → risk/kind policy
       read    (LOW)  → execute
       confirm (MED)  → HITL proposal → independent approver → execute
       HIGH / not enabled → deny (and stay out of the model-visible catalog)
  → audit (ai_tool_executions + NATS AI_EVENTS)
```

### Principles

1. The LLM only **proposes** tools. Authorization is decided server-side by the
   tools layer, never from model output, frontend data, or conversation state.
2. Permissions come exclusively from the authenticated `UserContext`: the BFF
   validates the session, strips client-supplied identity headers, and
   re-injects `X-User-Id`, `X-Tenant-Id`, `X-Permissions`. `ai-service` trusts
   these only after workload-identity verification (`x-service-auth` HMAC) and
   the gateway's `X-Auth-Checked` marker.
3. Every tool execution passes permission + risk policy and is audited, with
   redacted arguments/results (`policy_decision`, `error_code`, `tool_denied`
   events).
4. Approval is reserved for `confirm`-kind tools. Reads never require approval.

## What already exists (no new build required)

| Proposal element | Already implemented where |
|---|---|
| Server-side permission check per call | `internal/tools/types.go` `Registry.Resolve` — verifies every `RequiredPermissions` entry against `scope.Permissions`, with `superadmin` bypass |
| Identity from verified context only | `tools.Context` — gateway-injected identity, "never trusted from the client directly" (comment in types.go); workload HMAC in `internal/handler/service_auth.go` |
| Tool registry declaring permissions/risk | `Definition{Name, Version, Kind, RequiredPermissions, Risk, Timeout, RedactionProfile}` — generated from `contracts/ai-internal/*.json` + `internal/catalog/builtins.go` |
| LOW → execute | Kind `read` (crm.getCustomer, finance.getAccount, hrm.listEmployees, iam.me, iam.listCapabilities, knowledge.search) |
| MED → approval | Kind `confirm` → `ErrApprovalRequired` → HITL proposal (four-eyes: self-approval forbidden, 72 h expiry, idempotent execution, audit). Currently one allowlisted tool: `arda.crm.exportCustomer` |
| Audit every execution | `ai_tool_executions` per call (`policy_decision`, `arguments_redacted`, `result_redacted`) + NATS `tool_denied`, `sandbox_rejected`, `cross_tenant_attempt` |
| "LLM → 'user probably has permission' → execute" prohibition | Enforced structurally: model never sees or passes permissions; sandbox meta-`execute` re-resolves permissions per call server-side |

## Adjustments from the original proposal

1. **No separate "Authorization Service".** The system already has two
   authorization points: HTTP-route authorization in `auth-gateway`
   (`configs/policy.yaml`) and tool authorization in `ai-service` (tool
   catalog + `Registry.Resolve`). A third centralized service would duplicate
   both. The "Authorization Layer" is this pair, and the `Context` plumbing
   between them.
2. **HIGH-risk = not registered, not "registered but denied".** A tool the
   business has not approved should not exist in the runtime catalog at all
   (or carry `enabled: false` and be excluded from the model-visible catalog
   and the `search` meta-tool index). Keeping a denied tool visible only
   creates a false affordance for the model and burns context. Denial at
   runtime remains the backstop for revocations mid-session.
3. **`enabled` flag is adopted.** Generated catalogs gain an `enabled` boolean
   (from contract JSON); `enabled: false` tools are excluded from model-visible
   definitions and `search`, remain resolvable for audit/diagnosis. This is the
   one genuinely new mechanism in this ADR.
4. **Confirmation vs approval is one mechanism, not two.** MED risk maps to the
   existing `confirm` flow. If a lighter "requester confirmation in chat" is
   later wanted for non-sensitive MED actions, add it as a subtype of the same
   proposal/decision/execution state machine — do not build a parallel path.
5. **Export risk classification must match the security policy.** The internal
   security baseline (and the knowledge corpus) state that customer-data export
   requires manager approval, watermarking, and a 7-day expiry. Therefore PII
   export stays **HIGH → HITL approval** (current `confirm` classification);
   MED direct-execute applies only to aggregate/anonymized exports once the
   export ledger exists. The rule "approval is not the default for every
   export" holds at the tier level, not per specific PII export.
6. **Mutations are absent, not disabled.** No write tool is registered in the
   catalog today; `crm.customer.update`-style tools appear only in Phase 4B
   with idempotency keys (`ai_tool_executions.idempotency_key` already
   provisioned), resource versions, and rollback semantics.

## Phase plan (ratified)

### Phase 4A — permission-based tools (current state, to be completed)

- READ tools: permission → execute (done: crm/finance/hrm/iam + knowledge).
- EXPORT: PII → permission + HITL approval; aggregates → permission + audit
  (only after export ledger).
- MUTATE: not registered.
- Audit all executions (already flowing; add tenant-level dashboards later).

### Phase 4B — high-risk actions (when business needs them)

- Register the specific write tool with `Risk: HIGH`, idempotency contract,
  resource versioning, and rollback plan.
- Flow: permission → risk policy → HITL approval → execute, reusing the
  existing approvals state machine (`ai_approvals`: proposal → independent
  decision → execution → expiry).
- Acceptance tests before enabling: forbidden tool, approval expiry, double
  execution (idempotency), concurrent approvers, audit completeness
  (per AI-REFACTOR-PLAN.md Phase 4 verification list).

## Non-goals

- No runtime policy engine / condition DSL in Phase 4A; static per-tool risk
  and permission metadata is sufficient until a real use case demands
  context-dependent evaluation.
- No per-tenant override of tool risk tiers in Phase 4A.
