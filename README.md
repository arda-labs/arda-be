# Arda Backend

Go workspace for Arda backend services.

## Services

| Service | Responsibility |
| --- | --- |
| `apps/auth-gateway` | Auth edge/BFF, sessions, Kratos/Hydra proxy, forward-auth |
| `apps/iam-service` | IAM, users, roles, permissions, MFA, audit |
| `apps/finance-service` | Finance accounts, transactions, approvals |
| `apps/platform-service` | Platform reference data, parameters, lookups, organizations, geography |
| `apps/mdm-service` | MDM scaffold |

## Docs

- [Backend Current State](docs/backend-current-state.md)
- [Backend Roadmap](docs/backend-roadmap.md)
- [Platform Service](docs/platform-service.md)

## Direction

The backend remains HTTP/JSON at the edge and will evolve toward gRPC for internal service-to-service communication.

