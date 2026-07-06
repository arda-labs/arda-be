# AGENTS.md

## Cursor Cloud specific instructions

Durable, non-obvious notes for running this Go backend inside a Cursor Cloud VM.
Standard build/test/lint/run commands are documented in `README.md` and each
`apps/<service>/Makefile` (`make build|run|test|lint`); `finance-service` has no
Makefile, use `go` directly. The Go toolchain (`go 1.26.3`, see `go.work`) is
downloaded automatically via `GOTOOLCHAIN=auto`.

### The shared k3s LAN dev infra is NOT reachable from the Cloud VM
The committed `apps/*/configs/config.yaml` DSNs point at the shared cluster
(`192.168.100.201:30432` Postgres, Redis, Hydra `:30445`, Kratos `:30446`, Zeebe
`:30650`) and `auth.arda.io.vn`. From the Cloud VM these hosts accept the TCP
handshake but **reset the connection at the application layer**, so they cannot
be used. Do not rely on them; use the local Postgres below and override env vars.

### Local Postgres (provisioned in the VM snapshot)
- **PostgreSQL 18** is installed and used on `127.0.0.1:5432`. Version 18 matters:
  `iam-service` and `finance-service` migrations use the built-in `uuidv7()`
  function, which does **not** exist in Postgres 16.
- It is **not auto-started** on boot. Start it with: `sudo pg_ctlcluster 18 main start`.
- Per-service roles/databases are pre-created (persisted on disk): role
  `arda_<svc>` / password `123456`, databases `iam`, `platform`, `finance`,
  `crm`, `hrm`, `workflow`, `media`, `noti`.

### Running a service locally
Override `DATABASE_DSN` to point at local Postgres; migrations auto-apply on
startup (goose). Example:
```bash
cd apps/platform-service
DATABASE_DSN="postgres://arda_platform:123456@127.0.0.1:5432/platform?sslmode=disable" \
  go run ./cmd/platform-service        # HTTP :8091, gRPC :9091
```
- `iam-service` (HTTP :8080): set `HYDRA_ADMIN_URL=` and `KRATOS_ADMIN_URL=` empty
  and `REDIS_URL=` empty. The "provision superadmin identity skipped" Kratos
  warning is expected and non-fatal (Ory is unreachable). Migrations/casbin still load.
- `finance-service` (HTTP :8090): needs Postgres only; platform gRPC dial is optional.
- `platform-service` needs Postgres only — the simplest service to smoke-test
  (e.g. `POST /api/platform/organizations` then `GET /api/platform/organizations`).
- Services that need infra unavailable in the VM will not fully run:
  `workflow-service` (Zeebe), `media-service` (S3/Garage).

Individual service HTTP endpoints are unauthenticated at the service level; auth
is enforced by `auth-gateway`, which is not usable here because it validates
tokens against the unreachable Ory Hydra.
