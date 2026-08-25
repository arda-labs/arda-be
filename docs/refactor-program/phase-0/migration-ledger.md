# Phase 0 Migration Ledger

Status: initial ledger; every row needs an owner and exact operation/table scope.

| ID | Surface | Current state | Target wave | Prerequisites | Status |
| --- | --- | --- | --- | --- | --- |
| MIG-001 | gateway unknown policy routes | policy misses now return 404 before upstream | Phase 1 | SEC-01, ADR-004 | in_progress |
| MIG-002 | verified org context | gateway validates active org; FE sends selected org; CRM/media require scope | Phase 1 | SEC-02/03, ADR-003 | in_progress |
| MIG-003 | request/trace ID | request ID is persisted; HTTP/gRPC/FE propagation is covered | Phase 1/2 | BE-01, OBS-01 | in_progress |
| MIG-004 | public JSON success profiles | shared Go/TS success/problem helpers added; IAM permissions and browser `/api/auth/me` pilots now use the canonical envelope; endpoint migration remains | Phase 2/3 | API-01..05, FE-01..03 | in_progress |
| MIG-005 | public problem errors | migrated media/domain surfaces emit and consume `application/problem+json`; remaining legacy/provider errors are explicitly cataloged | Phase 2/3 | API-03, FE-03 | in_progress |
| MIG-006 | FE auth/raw fetch | auth bootstrap, logout and step-up use shared transport; `/api/auth/me` unwraps the canonical `result`, while Kratos/OAuth calls remain explicit credentialed protocol adapters | Phase 1/2 | FE-01/04/05 | in_progress |
| MIG-007 | pilot paginated list | IAM permissions switched to canonical `result` envelope and typed FE adapter | Phase 3 | API/SEC/FE foundations | in_progress |
| MIG-008 | pilot target command | admin MFA reset or equivalent | Phase 3 | security assurance/audit/idempotency | candidate |
| MIG-009 | pilot media lifecycle | tenant/org-scoped multipart and presigned init/complete upload, gRPC-only attach, retrieve and delete boundary added | Phase 3 | Media REST/gRPC contracts, outbox/storage | in_progress |
| MIG-017 | HRM canonical JSON | reference-data and employee-registration handlers switched directly; FE unwraps `result` | Phase 3/5 | API-01..05, FE-01..03 | in_progress |
| MIG-018 | Finance canonical JSON | ledger/operation/approval/config helpers switched directly; finance operation mock fallback removed | Phase 3/5 | API-01..05, FE-01..03 | in_progress |
| MIG-019 | IAM target tenant | admin user/group/role create, management lists and all target path operations require explicit `tenant_id`; a verified actor may manage same-tenant resources, while cross-tenant scope requires explicit `SUPER_ADMIN/superadmin`; repositories enforce target tenant on CRUD and user/group/role/permission mappings; `/me` remains actor-bound; source fallbacks to legacy `default` are removed and new IAM writes are blocked from recreating it | Phase 4 | SEC-04, DOM-IAM | in_progress |
| MIG-020 | Workflow canonical JSON | workflow handlers and workflow/workbench FE transport switched; XML/SSE remain native | Phase 6 | API-01..05, FE-01..03 | in_progress |
| MIG-021 | Notification canonical JSON | inbox, unread, read and push APIs switched; SSE remains native | Phase 6 | API-01..05, FE-01..03 | in_progress |
| MIG-022 | Platform canonical JSON | platform handler and FE adapter switched, including organization list profile | Phase 4/7 | API-01..05, FE-01..03 | in_progress |
| MIG-023 | IAM admin canonical JSON | admin user/group/role/permission/audit handlers and MFE adapter switched directly | Phase 4 | API-01..05, FE-01..03, DOM-IAM | in_progress |
| MIG-024 | IAM self-service canonical JSON | profile, session/device and MFA handlers plus auth-gateway/account/media consumers switched directly | Phase 4 | API-01..05, FE-01..03, DOM-IAM | in_progress |
| MIG-010 | finance idempotency/scope | scoped unique key and lookup use tenant + operation + key; request hash detects same-key/different-payload conflicts; empty tenant/actor rejected; new migration drops synthetic tenant defaults and adds non-empty checks pending historical-row assignment | Phase 5/7 | DATA-01/02, DOM-FIN | in_progress |
| MIG-025 | finance identifier scope hardening | account, balance, transaction and approval reads/mutations require explicit tenant predicates; handlers reject missing BFF tenant | Phase 5/7 | DATA-01/03, DOM-FIN | in_progress |
| MIG-026 | platform tenant defaults | organization, credit institution, area and file-template creation rejects empty tenant instead of writing synthetic `default`; new migration drops defaults and adds a non-empty check pending historical-row assignment | Phase 4/7 | DATA-01/03, DOM-PLT | in_progress |
| MIG-027 | HRM tenant isolation | HRM tables and repositories require verified tenant metadata; migration refuses to guess a tenant for existing rows and HTTP routes reject unverified scope | Phase 5/7 | DATA-01/03, DOM-HRM, SEC-02 | in_progress |
| MIG-028 | Workflow case scope | HTTP workflow routes require verified tenant context; case list/read/submit/claim/process-key/timeline queries enforce tenant predicates, while worker projection writes use an explicit internal method | Phase 6/7 | DATA-01/03, DOM-WFL, SEC-02 | in_progress |
| MIG-029 | Workflow role membership scope | Membership management requires explicit target `tenant_id`; claim resolution and work-item access use the case tenant; empty-string historical membership rows require explicit data assignment | Phase 5/6 | DOM-WFL, DATA-03 | in_progress |
| MIG-030 | Workflow delegation scope | Delegations are tenant-owned; list/create/update require explicit target `tenant_id`, repository predicates include tenant, and existing rows require explicit assignment before constraint validation | Phase 5/7 | DOM-WFL, DATA-03 | in_progress |
| MIG-031 | CRM/notification placeholder tenant removal | Customer tenant and notification outbox tenant defaults are removed; new writes reject empty or reserved `default`, while historical rows require explicit data-owner assignment before validation | Phase 5/6/7 | DATA-01/03, DOM-CRM, INT-04 | in_progress |
| MIG-011 | workflow idempotency propagation | `Idempotency-Key` now crosses CRM/HRM -> gRPC -> Workflow; tenant-scoped case keys, request hashes and a per-case submission lock are implemented; crash recovery and runtime duplicate-delivery evidence remain | Phase 6 | BE-06/07, DOM-WFL | in_progress |
| MIG-012 | internal service identity | mTLS transport plus signed workload assertion and destination allowlists are enforced on migrated gRPC paths, including Media and Notification; the auth-gateway IAM HTTP adapter remains an explicit private-boundary exception pending service identity/network-policy evidence | Phase 2/7 | ADR-006, INT-02 | in_progress |
| MIG-013 | event durability | notification outbox publishes through JetStream with PubAck and `Nats-Msg-Id`; bounded retry, DLQ storage and operator-gated one-event replay CLI are implemented; durable consumer acknowledgement and runtime replay evidence remain open | Phase 2/6/7 | ADR-007, BE-08, INT-04/05 | in_progress |
| MIG-014 | mixed ID/tenant types | UUID/VARCHAR/TEXT/default values | Phase 7 | DATA-01/03 | planned |
| MIG-015 | legacy `/api/v1` routes | CRM `/api/v1/customers` and Workflow `/api/v1/workflows/*` aliases removed after current FE consumer search found no supported callers | Phase 9 | ADR-002, consumer evidence | in_progress |
| MIG-016 | legacy FE adapters | per-domain/manual response assumptions | Phase 3-9 | pilot patterns, FE contract tests | planned |

## Ledger rules

- A row becomes `in_progress` only when a work-package ID and owner are assigned.
- A row becomes `switched` only with canary evidence and rollback readiness.
- A row becomes `removed` only after compatibility telemetry is zero/approved and
  a separate cleanup package is merged/deployed.
- Security/data/integrity rows cannot be waived by a compile-only result.
