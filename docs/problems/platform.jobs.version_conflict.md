---
code: platform.jobs.version_conflict
status: 409
title: Scheduled job changed
summary: |
  The scheduled job was modified by someone else after it was loaded, so this change was not saved.
client_action: |
  Reload the job, review the current settings and apply the change again.
operator_action: |
  None; this is an expected optimistic-concurrency rejection.
related_routes:
  - PATCH /api/platform/scheduled-jobs/{code}
---
