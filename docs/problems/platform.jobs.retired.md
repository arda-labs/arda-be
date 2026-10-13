---
code: platform.jobs.retired
status: 409
title: Scheduled job is retired
summary: |
  The scheduled job has been retired (it is no longer in platform-service's job
  catalog), so it cannot be run.
client_action: |
  Do not retry. Reload the scheduled-jobs list; retired jobs are kept only for
  run history.
operator_action: |
  If the job should still run, restore its definition in platform-service's job
  catalog and redeploy; otherwise no action is needed.
related_routes:
  - POST /api/platform/scheduled-jobs/{code}/run
---
