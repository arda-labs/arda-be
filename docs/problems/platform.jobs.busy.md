---
code: platform.jobs.busy
status: 409
title: Job scheduler busy
summary: |
  The scheduler has no free execution slot on this replica, or the service is
  shutting down, so the manual run was not started.
client_action: |
  Retry shortly. No run was recorded for this request.
operator_action: |
  If this persists, check the scheduler metrics for saturated slots (each
  replica runs at most 4 jobs at once) and for long-running or stuck runs.
related_routes:
  - POST /api/platform/scheduled-jobs/{code}/run
---
