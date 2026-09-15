# Human-in-the-loop

## Principle

HITL is a policy checkpoint, not merely a confirmation dialog. A frontend
approval card can improve UX, but only the backend may authorize and resume a
side effect.

The AG-UI runtime implements the pause/resume pattern natively: the backend
ends a run with a `RUN_FINISHED` `interrupt` outcome, the frontend collects a
response and submits it back through `useAgUiSubmitInterruptResponses`, and
the backend resumes the run on the same `/api/ai/agent` endpoint.
Arda must still enforce the checkpoint in Go and in the owning domain service.

## Required approval record

An approval request must include:

- run ID, conversation ID, tool name/version, and idempotency key;
- actor ID, active tenant ID, and permission snapshot/version;
- a redacted summary of the exact intended effect;
- resource IDs and a freshness/version token where applicable;
- expiry time, required approver capability, and current status.

The model cannot approve its own request. The UI cannot alter the resource,
tenant, amount, or arguments after the approval summary is created.

## Resume protocol

1. Server creates `PENDING_APPROVAL` and emits an approval event.
2. User sees the exact redacted effect and chooses approve, reject, or edit.
3. Server authenticates the resume request and re-checks tenant, permission,
   auth version, recent-auth, resource version, and expiry.
4. For an edit, the server validates a new typed proposal and creates a new
   approval record; it never mutates the old approval in place.
5. The owning service re-authorizes and executes with the idempotency key.
6. The AI run records the outcome and emits a final tool result.

## Approval classes

| Class | Examples | Phase 1 |
| --- | --- | --- |
| None | Knowledge answer, harmless formatting, read-only lookup | Allowed |
| Confirm | Create a draft, prepare an export, schedule a non-final task | Proposal + owner-execution implemented behind flag; still no real side effect |
| Strong confirm | Submit workflow, change business record, send external message | Disabled until reviewed |
| Step-up | Finance transfer/approval, role/MFA/security change | Must use IAM recent-auth/step-up and domain approval |

## Implemented proposal boundary

The Go AI service enforces a fail-closed HITL boundary for every `confirm`-kind
catalog tool. Enforcement lives in the sandbox engine (`sandbox.SDKMethod`
carries `RequiresApproval` propagated from `CatalogEntry.Kind`), so a
dispatcher can no longer decide for itself whether a mutation runs. With
`AI_ENABLE_HITL_PROPOSALS=true`:

1. When a script calls a `confirm`-kind method (currently the
   `crm.exportCustomer` stub), the engine raises the approval flow **before**
   the dispatcher executes, and the Code Mode suite persists a redacted
   `PENDING` approval with a deterministic idempotency key
   (`sha256(tenant|tool|arguments)`), pauses the run in `WAITING_APPROVAL`,
   and returns a typed `tools.ApprovalPending` result. The agent loop streams
   the proposal record inside the tool result and ends the run with an AG-UI
   `RUN_FINISHED` `interrupt` outcome.
2. An independent approver decides through
   `POST /api/ai/approvals/{id}/decision`; self-approval is rejected, expiry
   is enforced, and rejection finishes the run as `FAILED`.
3. Approval does not execute anything by itself. Only the **run owner** may
   resume through `POST /api/ai/approvals/{id}/execution` or the AG-UI resume
   entries, which re-resolve the tool against the catalog
   (`catalog.ExecutionResolver`, confirm-kind only, live permission check),
   execute the stored arguments within the original tenant scope, persist the
   tool result, and continue the agent loop.
4. If HITL is disabled or the approval store is unavailable, the tool call is
   refused with `approval_unavailable`; the service never fabricates a
   proposal id and never executes the mutation.

`crm.exportCustomer` still creates no export artifact — it verifies scope and
returns a bounded payload. Production keeps real side effects disabled until an
owning-domain executor design is approved.

The FE-initiated proposal endpoint (`POST /api/ai/approvals`) validates tool
names against `RouterOptions.ProposalTools`, which `main.go` builds from the
registered catalog, so the allowlist cannot drift from the tool registry.

## Safety rules

- Deny expired, duplicated, already-consumed, or permission-invalid approvals.
- Treat disconnect as an unresolved approval, not as approval.
- Never auto-approve on timeout.
- Keep approval and execution idempotent and observable.
- Do not include secrets or full sensitive payloads in the approval card or
  model transcript.
