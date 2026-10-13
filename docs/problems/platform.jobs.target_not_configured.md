---
code: platform.jobs.target_not_configured
status: 422
title: Scheduled job target not configured
summary: |
  The service that executes this job has no internal endpoint configured in
  platform-service, so the job cannot be run.
client_action: |
  Do not retry unchanged; contact an operator.
operator_action: |
  Configure the owner service's internal base URL for platform-service's job
  executor and make sure the service-auth secret is set, then redeploy.
related_routes:
  - POST /api/platform/scheduled-jobs/{code}/run
---
