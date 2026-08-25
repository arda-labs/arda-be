# Agent boundaries

## Capability model

The assistant has four distinct capabilities:

1. Explain: summarize approved knowledge and explain returned domain data.
2. Retrieve: search approved, ACL-filtered knowledge or invoke a named read tool.
3. Propose: prepare a draft, plan, filter, or command for the user to inspect.
4. Execute: perform an approved command only through a typed, authorized tool.

Phase 1 enables explain/retrieve and a small proposal surface. Execute remains
disabled until the tool has a domain contract, idempotency, audit, and HITL gate.

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
- Arbitrary SQL, shell, filesystem, network, code execution, or URL fetching.
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
