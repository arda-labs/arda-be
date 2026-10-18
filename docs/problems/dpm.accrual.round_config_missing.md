---
code: dpm.accrual.round_config_missing
status: 422
title: Product has no interest rounding configured
summary: |
  The daily deposit accrual skipped one or more savings accounts because their
  product has no ROUND_NO / ROUND_TYPE (CM163). The other accounts were accrued;
  the response fields list the skipped savings codes and how many were posted.
client_action: |
  Not an end-user action. Ask the product owner to enter the rounding step
  (round_no) and mode (round_type CM163.001, CM163.002 or CM163.003) through the
  product edit request, then rerun the accrual: the run is idempotent and makes
  up the skipped days.
operator_action: |
  Read `savings_codes` in the problem fields, find their products in
  `dpm_products` where `round_no IS NULL`, and have the values entered through
  the DPM product edit request (maker-checker). The migration deliberately leaves
  both columns NULL; nothing fills a default.
related_routes:
  - POST /internal/jobs/deposit-accrual-daily
---
