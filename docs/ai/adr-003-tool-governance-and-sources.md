# ADR-003: Tool governance, catalog sources, and the MCP boundary

**Status:** Accepted (with owner adjustments, 2026-09-15)
**Implementation status:** P0 + P1 landed (2026-09-15) — see the plan table below.
**Supersedes:** nothing
**Amends:** adr-002-tool-authorization.md (implements its `enabled` flag and defines the runtime override/clear semantics)
**Related:** catalog-scale-plan.md, code-mode-design.md, sdk-catalog-design.md, agent-evolution-roadmap.md

## Context

The AI tool surface has outgrown the current `/ai/tools` screen in `arda-mfe`.

1. **The catalog is real and already sizeable.** `ai-service` registers ~35
   entries: 30 generated from `contracts/ai-internal/*.json` across 12 domains
   (capital, crm, deposit, finance, hrm, iam, loan, mdm, notification,
   platform, statistical, workflow) plus 5 hand-written entries (`iam.me`,
   `iam.listCapabilities`, `crm.exportCustomer`, `knowledge.search`,
   `docs.problemLookup`). The dispatcher registry is the single source of
   truth; `GET /api/ai/tools` exposes it read-only
   (`internal/handler/router.go` `handleListTools`).
2. **The catalog UI does not scale.** `internal-tools-tab.tsx` hard-codes six
   domain filter values (`all`, `crm`, `finance`, `hrm`, `iam`, `knowledge`),
   so eight live domains cannot be filtered today. Cards render with no
   paging, no `enabled` state, and no permission awareness.
3. **There is no enable/disable control anywhere.** ADR-002 ratified an
   `enabled` flag as its one genuinely new mechanism (contract default,
   excluded from model-visible definitions and `search`, resolvable for
   audit/diagnosis). It is not implemented: no contract field, no database
   table, no runtime filtering.
4. **The MCP tab is a UI-only mock that misrepresents state.**
   `mcp-server-tab.tsx` renders three hard-coded servers with
   `status: "connected"` and invented tool counts, keeps additions in
   component state (lost on reload), and calls no API. `apps/ai-service`
   contains no MCP code. MCP has so far only been scoped as a *read-only
   exposure adapter* (Arda → external clients) deferred until a real consumer
   exists (`catalog-scale-plan.md` WP9, `agent-evolution-roadmap.md` M4.3).
   *Consuming external MCP servers* — what the tab depicts — has never been
   designed and carries a different security surface (egress, credentials,
   third-party tool trust).

## Decision

### 1. One catalog, one source of truth

The `ai-service` registry (generated + builtins) is the only authority for
tool inventory. Model context (`ModelSDKTypes`), the `search` index, the
approvals allowlist, and the admin UI all derive from that same registry; the
frontend never fabricates catalog rows, connection states, or counts.

### 2. Enable/disable is governance, not authorization

**State model — two inputs, one derived truth:**

| Input | Source | Meaning |
|---|---|---|
| `contractEnabled` | contract JSON `enabled` (absent = `true`), compiled into `generated.go` | Business/build approval. A tool the business has not approved (`false`) must never become enabled at runtime |
| `overrideEnabled` | `ai_tool_settings.enabled` (row absence = no override) | Operational kill switch written by the admin UI |

```text
effectiveEnabled = contractEnabled && (overrideEnabled ?? true)
```

- A row in `ai_tool_settings` (`method_name` PK, `enabled`, `updated_by`,
  `updated_at`) exists only while an override is in effect; deleting the row
  returns the tool to the contract default immediately.
- `contractEnabled = false` is a hard floor. The runtime override can only
  restrict, never enable beyond the contract — the same monotonic rule later
  applied to per-tenant overrides. In Phase 4A the override scope is
  platform-level.
- **Clear override:** `PATCH /api/ai/tools/{methodName}` accepts exactly one
  of `{"enabled": true|false}` (upsert the override) or
  `{"clearOverride": true}` (delete the row → contract default). The UI shows
  which layer is deciding (`Theo hợp đồng` vs `Ghi đè`) and offers a
  "Khôi phục mặc định" action whenever an override exists. Both set and clear
  are audited with `action: set|clear`, `previousEffective`, `newEffective`,
  and `updatedBy`.
- `effectiveEnabled = false` excludes the entry from model-visible surfaces
  (`ModelSDKTypes`, `search`, `ProposalTools`); `Registry.Resolve` rejects the
  call and audits `tool_disabled`. The entry stays visible in the admin
  inventory for diagnosis — the ADR-002 rule: absent from the model, not from
  the system.
- Changing the flag requires `ai.admin` / `superadmin` / `platform.manage`. It
  never replaces permission checks: authorization stays with
  `RequiredPermissions` and the delegated service scope.

### 3. Catalog UI is source-driven

`/ai/tools` remains the capability inventory and governance screen. Tabs are
by *source* (`Arda SDK`, MCP, frontend/A2UI actions) and appear only when a
source is real. Domain filters, counts, and chips are derived from the API
payload, not hard-coded; the screen is permission-aware (`hasPermission` from
`@workspace/auth`) and renders a dense table (tool, domain, kind, risk,
permissions, enabled toggle) with search/filter/paging so it stays usable at
100–200 entries.

### 4. MCP is an integration, not a catalog tab

- **Single-registry rule:** every MCP integration is a view/adapter over the
  one `DispatcherRegistry`; there is never a second catalog (no parallel DB
  table, no FE-local tool definitions, no separate enabled state).
  - *Exposure (WP9):* `tools/list` renders registry entries that are
    `effectiveEnabled` and visible to the caller; `tools/call` resolves through
    `Registry.Resolve` and re-checks permission, risk, and HITL. MCP never
    bypasses them. It is a configuration concern, not server CRUD.
  - *Consumption (external MCP servers):* blocked on a separate ADR that must
    cover egress allowlists, secret storage, tool-trust allowlisting, default
    risk classification (high/confirm), namespacing (`mcp.<server>.<tool>`),
    health/reconnect, and a per-server kill switch. Imported tools register as
    catalog entries with `source: "mcp"` and flow through the same governance,
    toggle, permission, and audit pipeline; the server record owns credentials
    and health only.
- **Interim:** remove the mock MCP tab, or keep a truthful feature-flagged
  empty state. No fabricated connection status or tool counts may ship.

### 5. API contract

- `GET /api/ai/tools` gains `enabled` (effective), `contractEnabled`,
  `overrideEnabled` (nullable), and `source`, plus optional filters (`q`,
  `kind`, `risk`, `enabled`) and paging; existing clients keep working.
- `PATCH /api/ai/tools/{methodName}` accepts exactly one of
  `{"enabled": true|false}` or `{"clearOverride": true}`, and **returns the
  updated catalog entry** — same shape as the GET item plus the derived
  fields, `updatedBy`, and `updatedAt` — so the FE updates its state from the
  response without refetching the list. A batch variant (deferred — see open
  questions) returns the list of changed entries.
- Gateway policy gains `ai-tools-write` (`risk: high`, permissions
  `ai.admin` / `superadmin` / `platform.manage`); every change is audited.

## Alternatives considered

| Option | Why rejected |
|---|---|
| Keep the MCP tab and "wire it later" | Ships fabricated state now; no consumer, no design, wrong information architecture (integrations vs catalog) |
| Per-tenant toggles in Phase 4A | No use case yet; permissions already scope per user; adds cache/audit complexity |
| Runtime hot-reload of the catalog from contracts | Already rejected in `sdk-catalog-design.md` §3.3; the toggle is an override layer, not a catalog reload |
| Registered-but-denied tools visible to the model | ADR-002 item 2: burns context and creates a false affordance; runtime denial stays the backstop for mid-session revocations |

## Consequences

Positive: one inventory, truthful UI, an admin kill switch that does not
require a redeploy, a one-step restore to the contract default, less
prompt/context waste, CI-auditable defaults.

Costs: one migration, one endpoint + policy, three derived fields in the tool
DTO, runtime filtering in `main.go`, a rebuild of the catalog tab, and
short-TTL cache or pub/sub to invalidate across pods.

Invariants to keep green:

1. A tool with `effectiveEnabled = false` never appears in `ModelSDKTypes`,
   `search`, or `ProposalTools`.
2. Toggling never bypasses `RequiredPermissions` or delegated-scope checks.
3. `scripts/check-ai-catalog.mjs` validates the contract `enabled` mapping and
   the generated `enabled` field.
4. FE keeps `page.tsx` ≤ 400 lines, i18n keys for all new labels, and no mock
   data.
5. The MCP adapter (either direction, when built) renders and resolves through
   the one registry; a second catalog or a second enabled state is a review
   blocker.

## Implementation plan

| Phase | Work | Gate |
|---|---|---|
| P0 (FE, ~0.5–1 day) | Remove/replace the mock MCP tab; derive domains from data; table + filters; permission-aware badges | No fabricated data; every existing tool still inspectable |
| P1 (BE+FE, ~2–3 days) | Contract `enabled` + catalog-gen; `ai_tool_settings` migration; effective-state filtering; PATCH (+ clear override) + policy + audit; toggle/restore UI; CI check | `go test ./...`, `check-ai-catalog.mjs`, typecheck green; a disabled tool disappears from model surfaces while `/api/ai/tools` still reports it, and clearing an override returns the tool to the contract default |
| P2 (event-driven) | MCP per separate ADR; source tabs when a second source is real | Separate ADR accepted |

## Non-goals

- No runtime catalog hot reload.
- No tenant self-service tool registration (the AI surface is a platform
  product, not a tenant feature).
- No MCP client runtime and no new agent framework.
- No gateway route or permission editing from this UI.

## Open questions (non-blocking; decide during P1)

1. Does the toggle need a reason/note field for the audit trail, or is
   `updated_by` + timestamp enough for Phase 4A?
2. Should disabled tools stay visible to users without admin permissions?
   Proposal: show them greyed so the surface is understandable, but render the
   switch only for admins.
3. Batch toggle by domain/filter in Phase 4A, or later? Proposal: later, after
   the audit shape is proven on single-tool changes.
