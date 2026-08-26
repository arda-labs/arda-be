# NATS Event Contracts — AI Service

Status: **Design specification — implement when NATS integration is enabled**.
Defines the AI domain events published to NATS for consumption by other Arda
services (IAM, Audit, Notification, Analytics).

---

## 1. Overview

AI service publishes events to NATS JetStream on two subjects:

```
arda.ai.runs.*         → Run lifecycle events (started, finished, failed)
arda.ai.approvals.*    → Approval lifecycle events (requested, decided, executed)
arda.ai.knowledge.*    → Knowledge lifecycle events (published, retired, expired)
arda.ai.audit.*        → Security-relevant AI events consumed by IAM audit
```

AI service **only publishes**. It does not subscribe to its own events.

Other services subscribe:
- `iam-service` → `arda.ai.audit.*` for security event correlation
- `notification-service` → `arda.ai.approvals.requested` for approval notifications
- `platform-service` → `arda.ai.runs.*` for usage/analytics aggregation

---

## 2. Envelope Schema

All events share a common envelope:

```json
{
  "specVersion": "1.0",
  "id": "01JZXXXXXXXXXXXXXXXXXXXXXX",
  "type": "ai.run.finished",
  "source": "arda://ai-service",
  "time": "2026-09-01T10:00:00.000Z",
  "tenantId": "tenant-abc",
  "actorUserId": "550e8400-e29b-41d4-a716-446655440000",
  "requestId": "req-xxxxxxxx",
  "traceId": "trace-xxxxxxxx",
  "runId": "run-xxxxxxxx",
  "data": { ... }
}
```

`id` uses UUIDv7 (time-ordered). `time` is RFC 3339 UTC.

---

## 3. Run Events

### `ai.run.started`

Published when a run transitions from `CREATED` to `RUNNING`.

```json
{
  "type": "ai.run.started",
  "data": {
    "conversationId": "conv-xxx",
    "agentId": "arda-assistant",
    "provider": "openai-gpt4o-mini",
    "modelId": "gpt-4o-mini",
    "protocolVersion": "1",
    "mode": "direct_tool"   // or "code_mode"
  }
}
```

### `ai.run.finished`

Published when a run transitions to `COMPLETED`.

```json
{
  "type": "ai.run.finished",
  "data": {
    "conversationId": "conv-xxx",
    "durationMs": 2340,
    "inputTokens": 2920,
    "outputTokens": 350,
    "toolCallCount": 1,
    "sandboxExecCount": 0,
    "mode": "direct_tool"
  }
}
```

### `ai.run.failed`

Published when a run transitions to `FAILED`.

```json
{
  "type": "ai.run.failed",
  "data": {
    "conversationId": "conv-xxx",
    "errorCode": "ai.model_unavailable",
    "durationMs": 3100,
    "retryable": true
  }
}
```

### `ai.run.cancelled`

Published when a user cancels a run (Stop button).

```json
{
  "type": "ai.run.cancelled",
  "data": {
    "conversationId": "conv-xxx",
    "cancelledBy": "user",
    "durationMs": 1200
  }
}
```

---

## 4. Approval Events

### `ai.approval.requested`

Published when an `ApprovalProposal` is created. `notification-service`
subscribes to send the approver an in-app notification.

```json
{
  "type": "ai.approval.requested",
  "data": {
    "approvalId": "appr-xxx",
    "toolName": "crm.customer.export.prepare",
    "summaryRedacted": "Export 120 khách hàng phân khúc Enterprise sang CSV",
    "requiredCapability": "ai.approval.execute",
    "expiresAt": "2026-09-01T10:30:00.000Z"
  }
}
```

**Note:** `summaryRedacted` must be safe to include in a notification. It must
not contain PII beyond what the approver is permitted to see, and must be
pre-cleared by the redaction policy in `ai-service`.

### `ai.approval.decided`

Published when an approver approves or rejects.

```json
{
  "type": "ai.approval.decided",
  "data": {
    "approvalId": "appr-xxx",
    "decision": "approve",      // or "reject"
    "approverUserId": "550e....",
    "selfApproval": false
  }
}
```

### `ai.approval.executed`

Published when the approved action is executed successfully.

```json
{
  "type": "ai.approval.executed",
  "data": {
    "approvalId": "appr-xxx",
    "toolName": "crm.customer.export.prepare",
    "durationMs": 450,
    "idempotencyKey": "sha256:xxxxxxxx"
  }
}
```

### `ai.approval.expired`

Published by a scheduled job when an approval passes its `expiresAt` without
a decision.

```json
{
  "type": "ai.approval.expired",
  "data": {
    "approvalId": "appr-xxx",
    "toolName": "crm.customer.export.prepare",
    "expiredAt": "2026-09-01T10:30:00.000Z"
  }
}
```

---

## 5. Knowledge Events

### `ai.knowledge.published`

Published when a source version moves to PUBLISHED.

```json
{
  "type": "ai.knowledge.published",
  "data": {
    "sourceId": "src-xxx",
    "version": 3,
    "scope": "tenant",
    "classification": "internal",
    "chunkCount": 42,
    "embedded": false    // true when vector embedding is complete
  }
}
```

### `ai.knowledge.retired`

```json
{
  "type": "ai.knowledge.retired",
  "data": {
    "sourceId": "src-xxx",
    "version": 2,
    "retiredBy": "superseded"  // or "admin" | "expired"
  }
}
```

---

## 6. Audit Events (Security-Relevant)

Consumed by `iam-service` for correlation with IAM audit log.

### `ai.audit.tool_denied`

Published when a tool call is rejected due to insufficient permissions.

```json
{
  "type": "ai.audit.tool_denied",
  "data": {
    "toolName": "finance.ledger.read",
    "missingPermission": "ai.tool.finance.read",
    "riskLevel": "medium"
  }
}
```

### `ai.audit.sandbox_rejected`

Published when a script fails static validation.

```json
{
  "type": "ai.audit.sandbox_rejected",
  "data": {
    "rejectionReason": "forbidden_identifier: eval",
    "scriptHash": "sha256:xxxxxxxx"
  }
}
```

### `ai.audit.cross_tenant_attempt`

Published when a detected cross-tenant access attempt is blocked.

```json
{
  "type": "ai.audit.cross_tenant_attempt",
  "data": {
    "attemptedTenantId": "other-tenant",
    "resolvedTenantId": "active-tenant",
    "toolName": "crm.getCustomer"
  }
}
```

---

## 7. NATS JetStream Configuration

```yaml
stream:
  name: AI_EVENTS
  subjects:
    - arda.ai.runs.>
    - arda.ai.approvals.>
    - arda.ai.knowledge.>
    - arda.ai.audit.>
  retention: limits
  max_age: 7d            # audit events retained 7 days in JetStream
  storage: file
  replicas: 3
  max_msg_size: 65536    # 64 KiB — no raw content in events
```

### Publishing policy

- AI service publishes with `AckWait: 5s`. If NATS is unavailable, the event
  is enqueued in a local ring buffer (capacity 1 000) and retried.
- A failed audit event publish **does not block** the primary operation (run,
  approval) from completing. Audit events are best-effort at the event bus
  level; `ai_tool_executions` and `ai_approvals` are the authoritative records.
- Events are published **after** the database write commits. Dual-write is
  avoided — the DB record is the source of truth.

---

## 8. Consumer Contracts

### `notification-service` — `ai.approval.requested`

Filter: `arda.ai.approvals.requested`

Action: Emit an in-app notification to users with the `ai.approval.execute`
capability in the active tenant. Include `summaryRedacted` and `expiresAt` in
the notification body. Link to the approval card in the AI panel.

### `iam-service` — `arda.ai.audit.*`

Filter: `arda.ai.audit.>`

Action: Append each event to the IAM security audit log with the shared
`requestId` and `traceId` for correlation. Do not mutate or re-emit the events.

### `platform-service` — run & approval events

Filter: `arda.ai.runs.>` and `arda.ai.approvals.>`

Action: Aggregate token usage, run counts, and approval rates per tenant per
day for the usage dashboard and billing attribution.
