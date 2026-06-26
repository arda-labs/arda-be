# Backend Roadmap

Last updated: 2026-06-27

Root long-term plan:

- [Backend gRPC Architecture & Long-Term Migration Plan](../../docs/roadmap/09-backend-grpc-architecture.md)
- [Backend gRPC Execution Checklist](../../docs/planning/backend-grpc-execution-checklist.md)
- [Dev Runtime Infrastructure, k3s, Redis, DB, and NATS](../../docs/roadmap/10-dev-runtime-infra.md)
- [Multilingual Platform & i18n Strategy](../../docs/roadmap/11-i18n-multilingual-platform.md)
- [Current k3s Cluster Inventory](../../docs/infra/current-k3s-cluster.md)
- [BPMN Direction: Camunda 8.8+ / Wait for 8.10](../../docs/architecture/bpmn-camunda8.md)

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
