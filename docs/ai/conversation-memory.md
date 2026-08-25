# Conversation and memory

## Separate three kinds of state

1. Transcript: user/assistant/tool messages needed to display and resume a run.
2. Run state: lifecycle, tool calls, approvals, model/provider metadata, and
   resumability for one execution.
3. Long-term memory: an explicitly approved preference or fact, stored only
   after a user-visible consent flow and with a clear owner/scope.

Phase 1 stores transcript and run state. It does not create implicit long-term
memory from arbitrary conversation text.

## Identity and tenant

Conversation ownership is `(tenant_id, actor_user_id)`. A conversation may be
shared only through an explicit Arda capability and membership check. The
active-tenant switch creates a new request context; it must not silently expose
another tenant's conversation or knowledge.

## Message policy

- Store role, sequence, content type, redacted content, and model/prompt version.
- Store tool arguments/results only through the redaction profile and size limit.
- Never store access tokens, cookies, passwords, MFA data, or hidden
  chain-of-thought.
- Treat uploaded files as separate classified sources, not as raw prompt blobs.
- Keep a bounded context window and summarize old messages with provenance.

## Retention and deletion

Initial defaults require product/legal confirmation:

- active conversations: 90 days;
- completed run/tool detail: 30 days;
- user feedback: 180 days;
- published knowledge: controlled by source owner/version policy;
- security-relevant audit: governed by IAM/security retention policy.

Retention must be implemented as an explicit, observable job. User deletion or
tenant offboarding must remove or anonymize AI records according to the same
data-governance decision used by the owning product.

## Resume and concurrency

- One active run per conversation unless a later design explicitly supports
  concurrent branches.
- Use monotonic message/run sequence numbers and idempotent resume requests.
- Reconnect can request a message/state snapshot; it must not replay a side
  effect.
- A stale browser tab cannot approve or resume a superseded run.
