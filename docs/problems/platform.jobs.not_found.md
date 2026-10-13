---
code: platform.jobs.not_found
status: 404
title: Scheduled job not found
summary: |
  No scheduled job exists with the given code, or no run exists with the given
  run id.
client_action: |
  Do not retry unchanged. Reload the scheduled-jobs list
  (`GET /api/platform/scheduled-jobs`) and use a code or run id from it.
operator_action: |
  Trace `request_id`. Confirm the job is registered in platform-service's job
  catalog and seeded in `plt_scheduled_jobs`, or that the run id exists in
  `plt_scheduled_job_runs` (runs are kept for 90 days).
related_routes:
  - GET /api/platform/scheduled-jobs/{code}
  - PATCH /api/platform/scheduled-jobs/{code}
  - POST /api/platform/scheduled-jobs/{code}/run
  - GET /api/platform/scheduled-jobs/runs/{id}
---
