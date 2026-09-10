---
code: ai.invalid_proposal_arguments
status: 400
title: Invalid proposal arguments
summary: |
  The `tool.arguments` of the export proposal failed validation: unknown
  fields, a blank or over-long `customerId` (max 128 chars), or a `format`
  that is not `csv` or `json`.
client_action: |
  Do not retry unchanged. Send `arguments` with exactly `customerId` and
  `format` (`csv` or `json`) and resend the proposal.
operator_action: |
  Trace `request_id` and inspect the `arguments` JSON in the proposal body —
  the decoder rejects unknown fields, and format matching is case-insensitive
  after lowercasing.
---

Emitted by `POST /api/ai/approvals/propose` for the only allowlisted tool,
`crm.customer.export.prepare` version 1. A tool name/version outside the
allowlist returns `ai.proposal_not_allowlisted` instead.
