---
code: workflow.case_type.unavailable
status: 422
title: Case type unavailable
summary: |
  The requested workflow case type cannot start because its step registry or
  worker capability is missing; starting it would strand the case with no
  inbox task.
client_action: |
  Do not retry the same submit. Surface the reason to the user and ask the
  operations team to complete the case-type rollout (registry steps, worker
  registration) before retrying.
operator_action: |
  Check that the workflow step registry was seeded for the case type
  (workflow_case_type_steps rows at the pinned registry_version) and that the
  domain worker group for its owner service is registered and healthy. Look at
  the `reasons` field of the problem payload and the workflow-service startup
  logs for registry seed failures.
related_routes:
  - POST /api/workflow/cases/{id}/submit
---

Case types are fail-closed: a case type whose registry or worker capability is
missing cannot be submitted. This keeps the "no fallback" contract — the
system never creates a workflow instance that no inbox can pick up.
