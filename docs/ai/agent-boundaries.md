# Agent boundaries

## Capability model

The assistant has four distinct capabilities:

1. Explain: summarize approved knowledge and explain returned domain data.
2. Retrieve: search approved, ACL-filtered knowledge or invoke a named read tool.
3. Propose: prepare a draft, plan, filter, or command for the user to inspect.
4. Execute: perform an approved command only through a typed, authorized tool.

"Execute" has two distinct forms that must not be confused:

- **Sandboxed Code Execution** (`execute` meta-tool): runs LLM-generated
  JavaScript inside an isolated Goja VM that can only call `arda.*` SDK methods.
  This is permitted and replaces individual sequential tool calls in Code Mode.
  It does not bypass HITL — mutation SDK methods inside the sandbox yield an
  `ApprovalProposal` instead of running the side effect.
- **Side Effect Execution** (mutation/confirm-kind tools): performs a real
  domain write, export, workflow action, or other irreversible operation. This
  always requires an approved HITL checkpoint and is disabled by default until
  the owning-domain contract, idempotency, audit, and HITL gate are complete.

Phase 1 enables explain/retrieve and a small proposal surface. Side Effect
Execution remains disabled until each tool has a domain contract, idempotency,
audit, and HITL gate. Sandboxed Code Execution is enabled as part of the
Code Mode rollout (Gate 7).

## Allowed data

- The current user's server-resolved identity and active tenant context.
- Knowledge sources explicitly published for the tenant or globally approved.
- Domain data returned by an allowlisted API/tool with resource-level filtering.
- Conversation messages and metadata needed to continue the current thread.

## Forbidden data and behavior

- Raw database credentials, model-provider keys, session cookies, MFA secrets,
  password hashes, private signing material, or internal service secrets.
- Direct queries against another service's database.
- Cross-tenant retrieval, even if a prompt asks for it.
- Arbitrary SQL, shell, host filesystem, unrestricted network, or unsandboxed
  process execution. Sandboxed JavaScript execution is permitted strictly within
  the isolated Goja runtime via the `execute` meta-tool, where scripts have zero
  OS/filesystem/network access and can only interact with the server-bound `arda.*` SDK.
- Sending email, changing permissions, moving money, changing MFA, deleting
  records, or submitting workflow approvals without an explicit approved tool
  and server-side HITL gate.
- Inventing live facts when a tool failed or returned no data.
- Presenting an unverified model citation as an Arda source.

## Trust hierarchy

When sources conflict, use this order:

1. Server authorization and domain-service response.
2. Explicitly published Arda policy/procedure with version and effective date.
3. Retrieved knowledge with source citation and freshness metadata.
4. User-provided context, clearly labeled as user-provided.
5. Model inference, clearly labeled and never used as a permission or execution
   decision.

## Failure behavior

- Missing identity/tenant: stop with authentication failure.
- Missing permission: stop before retrieval/tool/model work where practical.
- Retrieval unavailable: say that the knowledge source is unavailable; do not
  fabricate an answer.
- Domain tool unavailable: return a bounded error and request ID; do not retry
  unsafe commands automatically.
- Ambiguous intent: ask a clarifying question without calling a mutation tool.
- Policy uncertainty: deny or require a human/operator path; never fail open.
