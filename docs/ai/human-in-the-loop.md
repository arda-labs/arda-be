# Human-in-the-loop

## Principle

HITL is a policy checkpoint, not merely a confirmation dialog. A frontend
approval card can improve UX, but only the backend may authorize and resume a
side effect.

CopilotKit's `useHumanInTheLoop` is a useful UI pattern for pausing a run and
collecting a response; see the [v2 hook reference](https://docs.copilotkit.ai/reference/v2/hooks/useHumanInTheLoop).
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
| Confirm | Create a draft, prepare an export, schedule a non-final task | Spike only |
| Strong confirm | Submit workflow, change business record, send external message | Disabled until reviewed |
| Step-up | Finance transfer/approval, role/MFA/security change | Must use IAM recent-auth/step-up and domain approval |

## Implemented proposal boundary

The Go AI service has a disabled-by-default proposal boundary for the
non-production Gate 4 test. With `AI_ENABLE_HITL_PROPOSALS=true`, it accepts
only the typed `crm.customer.export.prepare` proposal (`customerId` plus
`csv`/`json` format), persists a redacted `PENDING` approval with an
idempotency key, and permits an independent approver to approve or reject it.
Approval does not create an export, call CRM, or resume a run; an owning domain
executor must be designed and separately approved before any side effect is
allowed. Production keeps this flag disabled.

## Safety rules

- Deny expired, duplicated, already-consumed, or permission-invalid approvals.
- Treat disconnect as an unresolved approval, not as approval.
- Never auto-approve on timeout.
- Keep approval and execution idempotent and observable.
- Do not include secrets or full sensitive payloads in the approval card or
  model transcript.
