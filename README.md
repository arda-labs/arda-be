# Arda Backend

Go workspace for Arda backend services.

## Services

| Service | Responsibility |
| --- | --- |
| `apps/ai-service` | AG-UI-compatible AI boundary: model agent loop, allowlisted tools, HITL approvals, conversation persistence |
| `apps/auth-gateway` | Auth edge/BFF, sessions, Kratos/Hydra proxy, forward-auth, AI proxy |
| `apps/iam-service` | IAM, users, roles, permissions, MFA, audit |
| `apps/finance-service` | Finance accounts, transactions, approvals, operation queues, accounting config reads |
| `apps/workflow-service` | Zeebe facade, business cases, workflow configuration, BPMN process definitions |
| `apps/platform-service` | Platform reference data, parameters, lookups, organizations, geography |
| `apps/crm-service` | Customers and membership workbench |
| `apps/hrm-service` | Positions, job titles, org units, employees, registrations |
| `apps/media-service` | Media assets on S3/Garage |
| `apps/notification-service` | Notification inbox and streams |
| `apps/mdm-service` | Master data management: currencies, countries, and other reference data under `/api/mdm/*` |
| `apps/loan-service` | Loan lifecycle: origination, collections, disbursements |
| `apps/deposit-service` | Deposit accounts and term deposits |
| `apps/capital-service` | Capital management domain |
| `apps/statistical-service` | Statistical reporting and analytics |

## Container ports

All services unify container ports at **HTTP 8080 / gRPC 9090** (commit `a3a8b136`).
Host ports in `docker-compose.yml` keep legacy mappings (`8090:8080`, `8091:8080`,
...) for local access. Bare-metal `go run` still uses each service's legacy
`configs/config.yaml` ports (e.g. platform `:8091`, crm `:8094`, mdm `:8096`,
loan `:8097`, ai `:8098`) — check the per-service config when running outside containers.

## Docs

- [Backend Current State](docs/backend-current-state.md)
- [Backend Roadmap](docs/backend-roadmap.md)
- [Kratos-first Identity Flow](docs/kratos-first-identity-design.md)
- [Auth User Context Contract](docs/auth-user-context-contract.md)
- [Tenant Context Contract](docs/tenant-context-contract.md)
- [HTTP Problem Details Catalog](docs/problems/README.md)
- [Deployment Namespace Layout](docs/deployment-namespace-layout.md)
- [AI Architecture and Phase Plan](docs/ai/README.md)
- [GHCR and k3s Deployment](docs/ghcr-k3s-deployment.md)
- Argo CD and Kubernetes manifests live in sibling repo `arda-infra`.
- [Platform Service](docs/platform-service.md)
- [Calendar & Cut-off Design](docs/calendar-cutoff-design.md)
- [CRM/HRM BPM Data Design](docs/crm-hrm-bpm-data-design.md)
- `apps/workflow-service/README.md`

## Direction

The backend is HTTP/JSON at the edge and gRPC (grpc-go with mTLS + signed workload assertions) for internal service-to-service communication across 11 proto domains under `proto/arda/**/v1`. BPM runtime targets Zeebe 8.5 through `workflow-service`; Arda owns the product UI in `arda-mfe`.
