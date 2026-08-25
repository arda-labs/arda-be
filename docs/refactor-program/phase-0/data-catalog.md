# Phase 0 Data Ownership and Risk Catalog

Status: initial catalog from service/migration/repository audit on 2026-08-25.

This document is a risk register, not a claim that every table has been fully
enumerated. The database owner must expand each family into table-level rows before
schema migration work starts.

## Required table-level fields

```text
service, database, schema/table, owner, primary_id_type, tenant_scope,
org_scope, foreign_keys, unique_constraints, indexes, money/time fields,
PII class, retention, soft_delete, version/concurrency, events,
read/write operations, migration risk, owner, status
```

## Proposed ownership map

| Service | Data families | Scope expectation | Initial status |
| --- | --- | --- | --- |
| IAM | users, identity mapping, roles, permissions, groups, sessions, devices, MFA, audit | tenant/user/org depending resource | expand table inventory |
| Platform | organizations, parameters, lookups, areas, geography, credit institutions, templates, calendar | global/reference or tenant/org by resource | expand constraints/index inventory |
| CRM | customers, member/profile, workbench and domain projections | tenant/org | verify all repository scope |
| HRM | positions, job titles, org units, employees, registrations | tenant/org; PII | verify lifecycle and PII |
| Finance | accounts, ledger, transactions, approvals, operation queues, accounting refs | tenant/org | high-risk remediation |
| Workflow | cases, process config, definitions, roles, memberships, assignments, delegations, SLA/timeline | mixed: cases/memberships/delegations tenant-owned; role catalog/assignment rules/process definitions service-global | high-risk remediation |
| Media | file metadata, upload sessions, visibility, processing/outbox | tenant/org/owner | high-risk access review |
| Notification | templates, preferences, inbox/delivery/outbox state | tenant/user | delivery and retention review |
| MDM | owned reference data to confirm | unknown | ownership decision required |

## High-priority findings from audit

| Area | Finding to validate | Risk | Planned package |
| --- | --- | --- | --- |
| Finance | global `idempotency_key` uniqueness/lookup may not include tenant + operation | duplicate/conflicting command semantics | DATA-02/DOM-FIN |
| Finance | several get/update/approval repository paths need tenant predicate review | cross-tenant access | SEC-03/DATA-02/DOM-FIN |
| Finance | ledger/account references may not enforce same-tenant relationship | data integrity | DATA-01/DOM-FIN |
| Media | public ID get/delete/download paths need tenant/org/visibility review | file disclosure/deletion | SEC-03/DOM-MED |
| Workflow | case get/update paths and submit idempotency propagation need review | cross-tenant/duplicate workflow | DATA-02/DOM-WFL |
| All | mixed UUID/VARCHAR/TEXT IDs and tenant types | joins/contracts/migrations | DATA-01/Phase 7 |
| All | magic/default tenant values | accidental unrestricted/shared data; historical rows require owner assignment and new writes must reject the reserved placeholder | SEC-03/DATA-01 |
| Config | tracked/default DB credentials | credential compromise | SEC-10/OPS-01 |
| Media | outbox rows/publisher behavior need durable ack/replay validation | event loss | BE-08/INT-04 |

## Query standard to validate

Every tenant-scoped repository operation must:

- accept explicit verified scope;
- include scope in read/update/delete predicates;
- use tenant-first indexes and deterministic ordering;
- distinguish not-found, forbidden scope and stale version safely;
- allowlist filter/sort fields;
- use transaction/lock/version semantics for state transitions;
- have cross-tenant negative tests.

## Migration classification

| Class | Examples | Migration rule |
| --- | --- | --- |
| A — contract only | add response metadata, error adapter | compatible deploy, no DB change |
| B — additive schema | nullable/new table/index | expand then switch |
| C — backfill | normalize IDs/tenant values or add derived field | checkpoint/reconcile before switch |
| D — invariant tightening | NOT NULL, unique scope, FK/check | pre-clean and validate all rows first |
| E — destructive cleanup | remove old column/table/route | last phase after compatibility window |

## Data catalog completion gate

- [ ] Every table has one owner and explicit scope.
- [ ] Every unique key is classified as global or scoped.
- [ ] Every resource ID/tenant/org type is recorded.
- [ ] Every money/time/PII field has a representation and retention rule.
- [ ] Every stateful aggregate has transition/concurrency notes.
- [ ] Every table has migration class, backup/recovery and validation plan.
