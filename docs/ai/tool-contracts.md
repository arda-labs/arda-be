# Tool contracts

## Contract shape

Every tool is a versioned server registry entry, not an arbitrary function name
chosen by the model.

```json
{
  "name": "crm.customer.get",
  "version": 1,
  "kind": "read",
  "description": "Read one customer in the active tenant",
  "input_schema": "...",
  "required_permissions": ["crm.customer.read"],
  "risk": "low",
  "timeout_ms": 3000,
  "idempotency": "not_required",
  "redaction_profile": "customer_summary"
}
```

The model receives only the name, description, and JSON schema. The server owns
the permission, tenant, timeout, retry, redaction, and downstream target.

## Execution pipeline (direct tool)

1. Resolve tool name/version from the server allowlist.
2. Validate JSON arguments with strict unknown-field rejection and bounds.
3. Resolve actor, tenant, roles, permissions, auth version, and risk context
   from trusted request metadata/session.
4. Check route/tool permission and resource scope.
5. For mutation/high-risk tools, create or resume a server approval checkpoint.
6. Call the owning service through its typed HTTP/gRPC contract.
7. Apply timeout, bounded retry, circuit-breaker, and redaction policy.
8. Persist outcome and emit AG-UI tool events plus audit/metrics records.

## Execution pipeline (Code Mode: `search` + `execute`)

1. Model calls `search(query, domain?)` — `ai-service` performs a keyword lookup
   over the indexed TypeScript SDK catalog and returns matching method signatures
   and JSDoc descriptions. No domain service is called at this step.
2. Model writes a JavaScript script using the discovered `arda.*` SDK methods and
   calls `execute(code)`.
3. `ai-service` validates the script (static checks: reject `eval`, `new Function`,
   prototype pollution patterns) before passing it to the Goja VM.
4. The Goja VM starts with a stripped global scope. Each `arda.*` method call
   inside the script routes through a Go dispatcher that:
   - re-checks the actor/tenant/permission context (zero client-supplied trust);
   - applies the same timeout, redaction, and bounded-result policy as a direct tool;
   - for mutation methods (`kind: "confirm"`), creates an `ApprovalProposal` and
     throws a catchable `ApprovalRequired` error into the script instead of
     executing the side effect.
5. The sandbox enforces a hard execution timeout (default 3 000 ms) via
   `vm.Interrupt()`. Memory and step limits prevent runaway scripts.
6. The final return value of the script (or the caught error) is bounded, redacted,
   and returned as the `execute` tool result.
7. Outcome, script hash, invoked SDK method names, duration, and status are
   persisted in `ai_tool_executions` for audit. Raw script source is stored
   only as a bounded, redacted reference; never raw in the model transcript.

## Initial tool allowlist

The initial production allowlist includes direct read-only tools and the 2 Meta-Tools for Code Mode:

- `search` (Meta-Tool): accepts `{"query":"...","domain":"crm|hrm|finance|knowledge|all"}`; returns TypeScript SDK signatures and JSDoc documentation on-demand.
- `execute` (Meta-Tool): accepts `{"code":"..."}`; executes sandboxed ECMAScript in an isolated Goja VM with `arda.*` SDK bindings, strict timeout (3000ms), and zero filesystem/network access.
- `knowledge.search`: accepts only `{"query":"...","limit":1..5}`, requires
  `ai.assistant.use` plus `ai.knowledge.read`, searches only published tenant,
  global, or system sources, and returns bounded chunks with source citations.
- `crm.customer.get`: accepts only `{"customerId":"..."}`, resolves tenant
  and organization scope from the gateway context, requires
  `ai.assistant.use` plus `crm.customer.read`, and returns a redacted summary
  without email, mobile, identity number, address, or arbitrary CRM JSON.
- `crm.customer.export.prepare` (kind `confirm`, behind the HITL flag):
  accepts only `{"customerId":"...","format":"csv"|"json"}`; the registry
  refuses direct execution, so requesting it produces an approval proposal
  that the run owner can later execute.

Every enabled definition carries its JSON Schema in `Definition.Parameters`,
which is what the model receives for function calling. See [code-mode-design.md](code-mode-design.md)
for the complete specification of the `search` and `execute` sandbox contract.

## Tool result rules

- Return typed result envelopes, not raw upstream JSON.
- Include `source`, `fresh_at`, and `request_id` when useful.
- Redact fields by policy before writing the transcript or sending model input.
- Limit rows, bytes, nesting, and execution time.
- Preserve domain error codes; do not expose stack traces, SQL, or policy internals.
- For empty results, return an explicit empty result; never let the model infer
  a positive fact from absence.

## Mutation rules

A mutation tool is not eligible until it has:

- an owning domain command contract;
- explicit permission and risk metadata;
- idempotency and conflict handling;
- a server-side HITL design;
- audit and rollback/forward-recovery behavior;
- tests for tenant escape, replay, timeout, and permission revocation.

## Tool event mapping

Use standard AG-UI tool lifecycle events where possible. Use Arda custom events
only for stable UI metadata such as `arda.approval.requested` or
`arda.citation.attachments`; document each event and keep payloads redacted.
