---
code: platform.jobs.already_running
status: 409
title: Scheduled job is already running
summary: |
  A run of this job is already in progress, so a second manual run was not
  started.
client_action: |
  Wait for the current run to finish (watch the job's run history), then
  trigger the job again if still needed.
operator_action: |
  If no run is actually executing, check that the run reaper is healthy; it
  marks runs past their timeout as failed and frees the job.
related_routes:
  - POST /api/platform/scheduled-jobs/{code}/run
---
