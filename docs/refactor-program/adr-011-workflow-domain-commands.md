# ADR-011 — Domain flows on the shared maker-checker through CaseCommandService

Status: accepted-with-follow-up (2026-10-12, wave 2 added 2026-10-13). Follow-up:
move the loan wave onto this contract; decide how flows with an `init` reserve
step and execute-time business failures (BUG-9) fit the shared process.

## Context

`workflow-service` is meant to be the only Zeebe facade. In practice it also
became the home of every domain's approval logic. Before this decision its
`internal/worker` package held 21 domain worker files (about 6 200 lines), it
dialled 9 domain services, and it registered 114 domain job topics. Each new
approval flow added a worker file, a client, a topic list and wiring in
`main.go`. The worker triples are nearly identical: call the owner service to
check, settle or reject, then complete the job and project the case. The HRM
registration worker and the loan adjustment worker differ only in RPC names.

T6.1 already introduced the shared `common-maker-checker` BPMN with static
`mc.validate|execute|cancel` topics, dispatched by the `mcKind` process
variable that comes from `business_operation_types.worker_kind`. The dispatcher
only knew how to reach loan-service, so moving another domain onto the shared
process would still have meant writing its handlers inside workflow-service.

## Decision

1. A new approval flow runs on `common-maker-checker`. It gets a
   `business_operation_types` row with `owner_service`, `worker_kind`,
   `maker_role` and `checker_role`, and no BPMN or worker code of its own.
2. The owner service implements `arda.common.v1.CaseCommandService`
   (`proto/arda/common/v1/case_command.proto`) once and serves every kind it
   owns:
   - `CheckCase(kind, object_id, case_id)` returns `ok=false` with a message
     for a business rule failure. Workflow throws `VALIDATION_FAILED` and the
     case returns to maker input. A gRPC error retries the job.
   - `ResolveCase(kind, object_id, case_id, decision, decided_by, note)` applies
     APPROVE or REJECT. It must be idempotent per `(kind, object_id, decision)`
     because Zeebe delivers jobs at least once. A gRPC error retries the job.
     `outputs` become process variables; reserved case variables are dropped.
   - Tenant, organisation and actor travel in gRPC metadata. Only
     workflow-service may call it.
3. Workflow routes each active kind to its `owner_service` and uses one generic
   transport (`internal/worker/case_command_workers.go`). It holds no business
   rule for these flows. Startup fails closed when an active kind has no owner,
   uses the `lnm.` namespace without being owned by loan-service, or names an
   owner without a configured gRPC address.
4. Side effects of an approval, including GL posting through finance
   `PostingService`, happen inside the owner service's `ResolveCase`, not in a
   workflow worker.
5. `scripts/check-workflow-workers.mjs` freezes the legacy surface: no new
   `*_workers.go` file, domain client import or domain job topic in
   workflow-service. Legacy items leave the baseline as their flows move.

Loan-owned kinds keep their current path (LoanWorkers and the batch workers)
until loan-service implements `CaseCommandService`. Nothing in this ADR changes
a running loan route.

## Alternatives considered

| Option | Why not |
|---|---|
| Owner services run their own Zeebe job workers | Every domain service would need Zeebe access (client, NetworkPolicy egress to `platform`, credentials) and a new API to update the case projection in the workflow database. More moving parts per service for the same result. |
| Keep adding per-domain workers in workflow-service | The status quo: workflow-service changes for every flow, and domain rules (for example posting leg construction) leak into it and into shared client libraries. |
| One RPC per domain flow (as `EmployeeCommandService`, `LoanCommandService` adjustments) | Works, but each flow still needs a workflow-side handler that knows the RPC names. The generic contract removes that. |

## Consequences

- Adding a flow touches the owner service, one registry row and, while canary
  discipline applies, one manual route script under
  `apps/workflow-service/migrations/canary/`. Workflow-service is untouched.
- Workflow-service still opens one gRPC connection per owner service that has
  an active kind; the addresses come from its existing `*_GRPC_ADDR` settings
  (`config.GRPCAddrFor`).
- A routing change is data. Workflow reads active routes at startup, so a
  route script is followed by a workflow-service restart.

## Pilot

`HRM_EMPLOYEE_REGISTRATION` (`worker_kind = hrm.employee-register`):

- hrm-service serves `CaseCommandService` by delegating to the existing
  registration logic. A repeated REJECT now succeeds when the registration is
  already REJECTED; the previous worker treated that retry as an error only by
  accident of ignoring the result.
- The route switch is
  `migrations/canary/20261012100000_workflow_hrm_employee_register_common_maker_checker_canary.sql`,
  applied manually after both images run; its Down refuses to run while
  common-process HRM cases are active.
- The legacy `hrm.employee.register.*` workers stay registered so in-flight
  instances on `hrm-employee-registration-v2` finish. Remove them, and their
  baseline entries, once no case uses that process.

## Checklist for a new flow

1. Owner service: add the kind to its `CaseCommandService` implementation;
   make `ResolveCase` idempotent; post to finance there if the flow posts.
2. Register `CaseCommandService` on the owner's gRPC server and allow
   `workflow-service` as caller.
3. Workflow registry: add the `business_operation_types` row
   (`bpmn_process_id = common-maker-checker`, `owner_service`, `worker_kind`,
   `maker_role`, `checker_role`), as a canary script while canaries are required.
4. Make sure workflow-service has the owner's `*_GRPC_ADDR`.
5. Tests: owner-side tests on PostgreSQL for check, approve twice, reject twice;
   a script test like `TestHRMEmployeeRegisterCanaryAppliesAndRollsBack`.

## Wave 2 (2026-10-13): crm, deposit, capital, statistical

Every three-step flow (validate, approve or reject, execute) owned by a
service other than loan-service now has a `CaseCommandService` adapter in
its owner and a manual route script under `migrations/canary/`. The legacy
workers and their baseline entries stay until the cases on the old
processes drain.

| Case type(s) | Legacy process | Owner | Worker kind | Status |
|---|---|---|---|---|
| HRM_EMPLOYEE_REGISTRATION | hrm-employee-registration-v2 | hrm-service | hrm.employee-register | pilot, applied on the dev cluster |
| CUSTOMER_REGISTRATION | crm-customer-registration-v2 | crm-service | crm.customer-register | moved |
| CRM_MEMBER_V1 | crm-member-v1 | crm-service | crm.member-request | moved |
| DPM_SETTLE_V2 | dpm-settle-v2 | deposit-service | dpm.settle | moved |
| DPM_PRODUCT_REGISTER_V1, DPM_PRODUCT_EDIT_V1 | dpm-product-register-v1, dpm-product-edit-v1 | deposit-service | dpm.product-request | moved (one script per legacy process) |
| DPM_RATE_REGISTER_V1, DPM_RATE_EDIT_V1 | dpm-rate-v1 | deposit-service | dpm.rate-request | moved |
| DPM_PAY_INTEREST_V1, DPM_CAPITALIZE_V1 | dpm-interest-v1 | deposit-service | dpm.interest-op | moved |
| DPM_BATCH_INTEREST_V1 | dpm-interest-v1 | deposit-service | dpm.interest-batch | moved; ops are read by `case_id` |
| IBM_PLACE_V1 | ibm-place-v1 | deposit-service | ibm.place | moved |
| IBM_BORROW_V1 | ibm-place-v1 | deposit-service | ibm.borrow | moved |
| IBM_TOP_UP_V1, IBM_INTEREST_V1, IBM_EXPECTED_V1, IBM_WITHDRAW_V1 | ibm-movement-v1 | deposit-service | ibm.top-up, ibm.interest, ibm.expected, ibm.withdraw | moved; the registry, not a variable, names the movement kind |
| CFC_CONTRACT_V1, CFC_AMENDMENT_V1, CFC_MOVEMENT_V1 | cfc-*-v1 | capital-service | cfc.contract, cfc.amendment, cfc.movement | moved |
| RPT_SUBMIT_V2 | rpt-submit-v2 | statistical-service | rpt.submit | moved |
| DPM_ADDITIONAL_V1 | dpm-additional-v1 | deposit-service | — | open: amount, date and idempotency key live only in process variables; needs a staged request first |
| CUSTOMER_ADJUSTMENT | customer-adjustment-v2 | crm-service | — | open: two terminal service tasks and no validate step; a different shape |
| CUSTOMER_RISK_REVIEW, FINANCE_INCOMING_TRANSACTION, FINANCE_OUTGOING_TRANSACTION | *-v1 | crm, finance | — | open: v1 seeds without a deployed BPMN; retire or redesign |
| FIN_* (7 flows) | fin-*-v2 | finance-service | — | open: `init` reserve step |
| LNM_* | lnm-*-v2 | loan-service | lnm.* (loan path) | unchanged until loan-service implements the contract |

Contract additions made for this wave:

- `ResolveCaseRequest.data_version` carries the row version the checker saw
  (the task form's `dataVersion` variable). Owners that guard rows keep
  their stale-approval check on the shared process; empty applies the
  decision unguarded as the legacy workers did for cases without a token.
- The generic transport records a failed check on the case timeline as
  `VALIDATION_FAILED`, as the CRM register worker did.
- Owners that lacked idempotency for a retried decision (statistical
  submission, deposit settlement and product request, IBM placement and
  borrow with a token) now re-read the row and accept one already in the
  state the decision produces. The opposite decision after a terminal
  state is `FailedPrecondition`; a stale token on a row still awaiting
  review is `Aborted`.
- On the shared process every flow starts at `UT_MakerInput` because
  `mcSkipMakerInput` is only set for batch disbursement. Flows whose own
  BPMN started at `ST_Validate` (customer and employee registration) gain a
  maker step before validation; their `UT_MakerRevise` step policies do not
  apply to `UT_MakerInput` and may need registry rows.

## Open follow-ups

- Flows with an `init` reserve step (disbursement, collection, batch, the seven
  `fin.*` flows) do not fit the three-step shared process yet.
- An execute-time business failure (for example a closed period) is retried
  and becomes an incident instead of returning to the maker (BUG-9). Decide
  whether `ResolveCase` gets a business-failure result and the shared BPMN an
  error boundary on `ST_Execute`.
- Loan adjustments move to `CaseCommandService` when loan-service implements
  it; then LoanWorkers and its per-kind topics leave the baseline.
- DPM_ADDITIONAL_V1 needs a staged request row in deposit-service before it
  can use the shared contract; CUSTOMER_ADJUSTMENT needs a shape decision.
- Owner-side approval paths that post to finance (dpm.interest-op,
  dpm.interest-batch, ibm.place, ibm movements, cfc.movement) are only
  covered on their idempotent already-applied paths in tests; a fake
  finance client would let the posting path run on PostgreSQL.
- Once a wave-2 kind is live, remove its legacy `*_workers.go`, client
  import and topics from `scripts/check-workflow-workers-baseline.json`.
