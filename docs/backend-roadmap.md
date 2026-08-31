# Backend Roadmap

Last updated: 2026-07-04

Root long-term plan. The detailed sub-documents below previously lived under
`docs/roadmap/`, `docs/planning/`, `docs/infra/`, and `docs/architecture/`
but were relocated during the refactor; they are kept here as references.

- **Backend gRPC Architecture & Long-Term Migration Plan** — see `docs/refactor-program/01-architecture-and-governance.md` and `docs/refactor-program/02-contracts-and-frontend.md`.
- **Backend gRPC Execution Checklist** — see `docs/refactor-program/08-templates-and-checklists.md`.
- **Dev Runtime Infrastructure, k3s, Redis, DB, and NATS** — see `docs/ghcr-k3s-deployment.md`, `docs/deployment-namespace-layout.md`, and `docs/dev/dependencies.md`.
- **Multilingual Platform & i18n Strategy** — see `arda-mfe/docs/conventions/i18n-and-localization.md`.
- **Current k3s Cluster Inventory** — see `arda-infra/README.md` and `.github/profile/README.md`.
- **BPMN on Camunda 8 — Modeling & Runtime Contract** — see `apps/workflow-service/README.md`.
- **Workflow Boundary Refactor Plan** — see `docs/refactor-program/04-data-and-service-integration.md`.

## Direction

Use HTTP/JSON at the edge and gRPC for internal service-to-service communication.

```txt
Frontend
  -> HTTP/JSON
  -> auth-gateway / Traefik
  -> services

Internal services
  <-> gRPC
```

This keeps browser/API ergonomics simple while giving backend services typed contracts, generated clients, deadlines, metadata propagation, and clearer internal boundaries.

## Why Not Replace HTTP Completely

HTTP/JSON should remain for:

- browser-facing APIs
- auth callback/login/session routes
- simple health checks
- external integrations
- manual debugging with curl/Postman

gRPC should be used for:

- service-to-service reads/writes
- high-volume internal calls
- typed internal contracts
- workflows that call multiple services
- shared platform/IAM lookups from business services

## Target Backend Shape

Each business service should eventually look like this:

```txt
apps/<service>/
  cmd/<service>/
  internal/domain/
  internal/repository/
  internal/service/
  internal/transport/http/
  internal/transport/grpc/
  internal/client/
  migrations/
```

Business logic should live under `internal/service`. HTTP handlers and gRPC servers should both call the same service layer.

## Proto Strategy

Use the existing root `proto/` folder for source `.proto` files:

```txt
proto/
  arda/
    platform/
      v1/platform.proto
    iam/
      v1/iam.proto
    finance/
      v1/finance.proto
```

Generated Go code can either be:

- committed under `libs/go/arda-proto`, preferred once contracts stabilize
- generated inside each service while contracts are still moving

Preferred long-term package:

```txt
libs/go/arda-proto/
  platform/v1/
  iam/v1/
  finance/v1/
```

## First gRPC Milestone

Start with `platform-service` because it owns shared reference data and will naturally be called by many services.

Milestone 1:

- Add `proto/arda/platform/v1/platform.proto`
- Generate Go stubs
- Add `internal/transport/grpc`
- Keep current HTTP routes
- Add gRPC methods for:
  - list/upsert parameters
  - list lookup categories
  - list lookup values
  - list organizations
  - list administrative units

Milestone 2:

- Add a platform gRPC client package
- Make `finance-service` call `platform-service` through gRPC for organization/branch/reference data
- Add request metadata propagation:
  - `x-request-id`
  - `x-user-id`
  - `x-tenant-id`
  - `x-roles`
  - `x-permissions`

Milestone 3:

- Add IAM gRPC internal API for user context and permission checks
- Move auth-gateway internal IAM calls from ad hoc HTTP client to generated gRPC client

## Workflow Boundary Refactor

Target rule:

```txt
Only workflow-service talks to Zeebe.
Domain services talk to workflow-service by gRPC.
Domain and workflow side effects are published through NATS.
```

Near-term phases:

1. Add `workflow.v1` proto for case/task commands.
2. Move CRM/HRM workflow submit calls from ad hoc HTTP to workflow gRPC.
3. Move Zeebe workers currently hosted by domain services into `workflow-service`.
4. Make workflow workers call domain services through domain gRPC commands.
5. Publish workflow/domain side effects through NATS for notification, audit, and projections.

`zeebe_addr` belongs in `workflow-service` only.

## Cross-Cutting Standards

Every internal call should carry:

- request ID
- tenant ID
- user ID or service identity
- source service
- locale, when rendering/display data is involved
- deadline/timeout

Every service should expose:

- `/health/live`
- `/health/ready`
- HTTP edge routes where needed
- gRPC health service once gRPC is added

Every database-backed service should use:

- embedded goose migrations
- env override for `DATABASE_DSN`
- no hardcoded secrets beyond local development defaults

## Open Decisions

- Whether generated proto code is committed immediately or generated in CI.
- Whether to use ConnectRPC for HTTP/gRPC compatibility or plain `grpc-go`.
- Whether service-to-service authorization uses IAM permission strings, SPIFFE/service identity, or both.
- Whether platform reference data should publish cache invalidation events later.
