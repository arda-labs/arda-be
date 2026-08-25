# Phase 0 — Baseline and Decision Package

Status: baseline captured; implementation has started under the Phase 1/2 gates.

Snapshot date: 2026-08-25

This folder is the working package for GOV-01 through GOV-07. It records what is
known from source inspection, what still needs owner validation, and which ADRs
must be accepted before implementation phases begin.

## Scope

Phase 0 covers:

- backend HTTP route and gateway inventory;
- frontend API consumer and raw transport inventory;
- database/data ownership and risk inventory;
- synchronous and asynchronous service topology;
- correctness/performance/test baseline;
- ADR register and decision owners;
- first pilot selection and implementation prerequisites.

The catalogs remain draft until owner validation, but approved containment and
shared-foundation work may proceed with explicit compatibility and test evidence.

## Files

| File | Work package | Status |
| --- | --- | --- |
| [endpoint-catalog.md](endpoint-catalog.md) | GOV-01 | draft from source scan; owner validation required |
| [frontend-consumer-catalog.md](frontend-consumer-catalog.md) | GOV-02 | draft from MFE scan; feature owners required |
| [data-catalog.md](data-catalog.md) | GOV-03 | initial risk inventory; DB owner validation required |
| [service-topology.md](service-topology.md) | GOV-04 | initial topology; runtime confirmation required |
| [baseline-report.md](baseline-report.md) | GOV-07 | evidence from current audit; refresh before Phase 1 |
| [adr-register.md](adr-register.md) | GOV-05 | proposed decisions; approval required |
| [migration-ledger.md](migration-ledger.md) | GOV-06 | initial waves and pilot candidates |

## Source snapshot inspected

Backend:

- `arda-be/apps/*/internal/transport/http/router.go` and MDM `cmd` router;
- auth-gateway BFF proxy and policy configuration;
- `arda-be/libs/go/*` technical libraries and `proto/`;
- service repository, migration, gRPC client/server and event code from the audit.

Frontend:

- `arda-mfe/apps/*/src/features/**/api.ts` and workbench adapters;
- `arda-mfe/packages/api`, `auth`, `media`, `admin-list`, `query`, `i18n`;
- shell proxy configuration and Module Federation shared configuration.

Infrastructure/performance:

- `arda-infra` edge/Traefik/auth/database/GitOps documentation and manifests;
- `arda-perf/api-load.js`, runner and current performance notes.

## Current high-confidence findings

1. auth-gateway has an explicit `/api/` generic proxy; policy matching must be
   proven deny-by-default for every downstream route.
2. Backend legacy `/api/v1` workflow/customer aliases were removed; the
   canonical paths are now the only supported public routes.
3. MFE has domain API adapters, but auth/bootstrap paths and some feature flows
   still use raw `fetch` outside the shared client.
4. Response shapes are not uniform across all routes despite existing convention docs.
5. Organization response/guard assumptions need a contract test before migration.
6. Tenant/org scoping and service identity are not yet uniform across repositories
   and internal calls.
7. Internal events/outbox/publisher acknowledgement and consumer dedupe need one
   operational contract.
8. Existing worktree modifications must be preserved and are not part of this phase.

## Phase 0 completion checklist

- [ ] Every route family has an owner and exact source location.
- [ ] Every FE API consumer maps to an endpoint operation and response profile.
- [ ] Every table family has owner, scope, ID type, PII class and migration risk.
- [ ] Every internal call/event has protocol, deadline, retry, identity and trace status.
- [ ] Baseline commands/results are reproducible and stored.
- [ ] ADR-001..010 have accepted/rejected/deferred decisions and owners.
- [ ] Three pilots have selected resources and rollback criteria.
- [ ] Phase 1/2 work packages have no unresolved contract dependency.

## Stop condition

If an implementation request requires choosing a value that is still `proposed` in
the ADR register, stop and create/approve the decision first. Do not let a subagent
silently encode a temporary architecture choice in shared code. Implemented
foundations must be marked as accepted-with-follow-up in the ADR/ledger before
the next dependent wave starts.
