---
code: calendar.error.eod_unavailable
status: 503
title: End-of-day service unavailable
summary: |
  The SYSTEM business-date calendar or EOD orchestrator could not be reached, so the requested transition did not complete.
client_action: |
  Retry after service availability is restored. Do not submit dependent business work against a presumed new business date.
operator_action: |
  Trace `request_id`, check platform-service health and its authenticated IAM tenant-list RPC, then inspect the EOD run and business-date status before retrying.
related_routes:
  - POST /api/platform/calendar/eod
  - POST /api/platform/eod/run
---
