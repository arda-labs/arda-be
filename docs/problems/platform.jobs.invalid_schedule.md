---
code: platform.jobs.invalid_schedule
status: 400
title: Invalid job schedule
summary: |
  The cron expression or timezone is invalid, or a numeric setting (timeout,
  max attempts, retry backoff) is out of range.
client_action: |
  Fix the highlighted value and resubmit. Use a 5-field cron expression and an
  IANA timezone such as `Asia/Ho_Chi_Minh`; `timeout_seconds` is 10..3600,
  `max_attempts` 1..5 and `retry_backoff_seconds` 0..3600.
operator_action: |
  None; this is a client validation error.
related_routes:
  - PATCH /api/platform/scheduled-jobs/{code}
  - POST /api/platform/scheduled-jobs/cron-preview
---
