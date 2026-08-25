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

## Execution pipeline

1. Resolve tool name/version from the server allowlist.
2. Validate JSON arguments with strict unknown-field rejection and bounds.
3. Resolve actor, tenant, roles, permissions, auth version, and risk context
   from trusted request metadata/session.
4. Check route/tool permission and resource scope.
5. For mutation/high-risk tools, create or resume a server approval checkpoint.
6. Call the owning service through its typed HTTP/gRPC contract.
7. Apply timeout, bounded retry, circuit-breaker, and redaction policy.
8. Persist outcome and emit AG-UI tool events plus audit/metrics records.

## Initial tool allowlist

The first production allowlist should be small and read-only, for example:

- `knowledge.search` — approved tenant/global knowledge with citations.
- `crm.customer.get` — one customer summary in the active tenant.
- `workflow.case.get_status` — status of a case the user may already read.
- `platform.lookup.search` — approved reference data only.

The exact names and permissions require an implementation PR and contract
review. No tool is enabled merely because it appears in this document.

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
