# Security and permissions

## Trust boundaries

The browser is an untrusted client. The gateway-injected context is trusted only
because the internal boundary authenticates the gateway/service call. The model
is untrusted input/output and never a security principal.

The AI service must authenticate internal calls with the existing Arda service
identity/mTLS or approved service-auth mechanism. Network placement alone is not
authorization.

## Permission model

Use explicit permission codes owned by IAM, for example:

- `ai.assistant.use` — start a conversation/run;
- `ai.knowledge.read` — retrieve approved knowledge;
- `ai.tool.<domain>.<operation>` — invoke a named tool;
- `ai.approval.execute` — approve a particular action class.

These names are proposed and require alignment with IAM permission conventions.
Do not grant `ai.*` wildcard access to normal users.

Every tool additionally checks resource and tenant scope. A permission check at
the gateway is necessary but not sufficient for a tool or domain mutation.

## Risk policy

Use the existing auth risk model:

- low: local/session validation and bounded context cache may be acceptable;
- medium: fresh context when stale and explicit tool policy;
- high: fresh IAM check, recent-auth, configured MFA/step-up, domain approval,
  and immutable audit.

AI reads that expose financial, HR, or identity data should default to medium or
high until classified. A prompt cannot lower route/tool risk.

## Secrets and provider controls

- Store model credentials only in the cluster secret mechanism and inject them
  into the AI service; never commit them or send them to the MFE.
- Allowlist provider hosts and models; set timeouts, token budgets, and cost
  limits.
- Log provider/model IDs and usage metadata, not prompts containing secrets.
- Do not send more data to a provider than the selected tool/RAG policy allows.
- Define provider outage behavior before enabling production traffic.

## Fail-closed requirements

Reject the request when actor, tenant, permission, service authentication,
approval, or source ACL is missing or ambiguous. A model timeout, provider
error, or audit write failure must never turn into a successful mutation.

## Security test cases

- spoofed `X-User-*`, tenant, role, permission, or auth headers;
- cross-tenant conversation ID, source ID, and resource ID;
- revoked permission/auth version during a paused approval;
- prompt injection in a knowledge document;
- tool schema extra fields, oversized inputs, replayed idempotency keys;
- provider timeout, partial stream, browser reconnect, and duplicate resume;
- redaction checks for logs, audit, transcript, metrics, and error responses.
