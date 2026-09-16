---
code: ai.approval_arguments_invalid
status: 409
title: Approval arguments are not executable
summary: |
  The stored original arguments of the approved proposal are missing, cannot be
  decrypted, are not valid JSON, or exceed the executable size bound. Running
  them could execute a different action than the one the approver reviewed.
client_action: |
  Do not retry. Create a new proposal so a fresh, valid payload is reviewed and
  approved before execution.
operator_action: |
  Inspect `ai_tool_executions.arguments_encrypted` for the approval and the
  service encryption secret configuration. Legacy rows are backfilled from
  `arguments_redacted`, which may be truncated; such an approval must be
  re-created rather than executed.
---

Emitted by `POST /api/ai/approvals/{id}/execution` and the AG-UI resume entries
on `POST /api/ai/agent` when `FetchApprovedExecution` cannot resolve a bounded,
valid JSON payload. The approval is not consumed and the tool never runs.
