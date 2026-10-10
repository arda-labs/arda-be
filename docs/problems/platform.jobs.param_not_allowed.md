---
code: platform.jobs.param_not_allowed
status: 400
title: Parameter not allowed for job
summary: |
  A manual run supplied a parameter that this job does not accept.
client_action: |
  Remove the parameter, or use only the parameters the job declares, and run
  the job again.
operator_action: |
  None; allowed parameters are declared in platform-service's job catalog.
related_routes:
  - POST /api/platform/scheduled-jobs/{code}/run
---
