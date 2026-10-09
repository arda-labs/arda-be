# Document numbering

`libs/go/arda-docno` is the shared allocator for business display numbers. It
keeps the immutable `document_id` separate from the mutable `display_no` and
accepts the canonical business date as an explicit input. The library does
not read the host clock or select a series implicitly.

## Allocation contract

- A caller resolves an active `doc_series` and begins a transaction in the
  same database as the document it is creating.
- Call `docno.Issue(ctx, tx, series, documentID, businessDate)`, write the
  business document using that same transaction, then commit. Roll back the
  whole transaction on any error. This makes the counter increment and number
  reservation atomic with the business write.
- `reset_period` is `NONE`, `YEAR`, `MONTH`, or `DAY`. The period key is derived
  from the supplied date. Patterns use `{YYYY}`, `{MM}`, `{DD}` and exactly one
  `{SEQ:n}` token. The date tokens must match the reset period.
- Overflow is rejected as `DOC_SEQ_OVERFLOW`; sequence text is never
  truncated. Gaps are allowed and can be documented with `RecordGap`.
- `doc_number_alias` reserves active and retired display numbers in one
  tenant-wide namespace. A retired number remains an alias for its immutable
  document and cannot be reused.

## Renumber review

Finance exposes `POST /api/finance/document-numbers/renumber-requests` with
`document_id`, `display_no`, and a required `reason`. A different user approves
at `POST .../renumber-requests/approve`. A closed-period change uses
`POST .../renumber-requests/approve-closed`, protected by
`doc.renumber.locked`; the ordinary approval endpoint rejects closed periods.
The approval transaction reserves the replacement number, retires the old
number, updates the document's display number, and records the checker. It
does not update journal entries or lines.

Permissions are registered by the IAM migration and are intentionally not
assigned to business roles here. No document series or document-type rule is
seeded without an approved business configuration. Existing posting number
generation remains unchanged; integration is scheduled for T3.5.

## Document type rules

`ResolveDocumentTypeRule` selects the lowest-priority matching organization
rule. A GLOBAL rule is considered only when the caller explicitly enables the
fallback, and that choice is logged. Missing configuration returns
`DOC_TYPE_RULE_NOT_FOUND`.
