# AGENTS.md

## Cursor Cloud specific instructions

Durable, non-obvious notes for running this Go backend inside a Cursor Cloud VM.
Standard build/test/lint/run commands are documented in `README.md` and each
`apps/<service>/Makefile` (`make build|run|test|lint`); `finance-service` has no
Makefile, use `go` directly. The Go toolchain (`go 1.26.3`, see `go.work`) is
downloaded automatically via `GOTOOLCHAIN=auto`.

### The shared k3s LAN dev infra is NOT reachable from the Cloud VM
The committed `apps/*/configs/config.yaml` DSNs point at the shared cluster
(`192.168.10.201:30432` Postgres, Redis, Hydra `:30445`, Kratos `:30446`, Zeebe
`:30650`) and `auth.arda.io.vn`. From the Cloud VM these hosts accept the TCP
handshake but **reset the connection at the application layer**, so they cannot
be used. Do not rely on them; use the local Postgres below and override env vars.

### Local Postgres (provisioned in the VM snapshot)
- **PostgreSQL 18** is installed and used on `127.0.0.1:5432`. Version 18 matters:
  `iam-service` and `finance-service` migrations use the built-in `uuidv7()`
  function, which does **not** exist in Postgres 16.
- It is **not auto-started** on boot. Start it with: `sudo pg_ctlcluster 18 main start`.
- Per-service roles/databases are pre-created (persisted on disk): role
  `arda_<svc>` / password supplied from the local secret file, databases `iam`, `platform`, `finance`,
  `crm`, `hrm`, `workflow`, `media`, `noti`.

### Running a service locally
Override `DATABASE_DSN` to point at local Postgres; migrations auto-apply on
startup (goose). Example:
```bash
cd apps/platform-service
DATABASE_DSN="postgres://arda_platform:<password>@127.0.0.1:5432/platform?sslmode=disable" \
  go run ./cmd/platform-service        # HTTP :8091, gRPC :9091
```
- `iam-service` (HTTP :8080): set `KRATOS_ADMIN_URL=` empty when Ory is
  unavailable. Migrations/casbin still load; browser auth is owned by
  `auth-gateway`.
- `finance-service` (HTTP :8090): needs Postgres only; platform gRPC dial is optional.
- `platform-service` needs Postgres only — the simplest service to smoke-test
  (e.g. `POST /api/platform/organizations` then `GET /api/platform/organizations`).
- Services that need infra unavailable in the VM will not fully run:
  `workflow-service` (Zeebe), `media-service` (S3/Garage).

Individual service HTTP endpoints are unauthenticated at the service level; auth
is enforced by `auth-gateway`, which is not usable here because it validates
tokens against the unreachable Ory Hydra.

---

## 7. AI assistant (Olorin) stack notes

* Backend boundary is Go-native: `ai-service` serves the CopilotKit envelope
  (`POST /api/copilotkit`, methods `info` + `agent/run`). The Node `ai-runtime`
  adapter is retired. Contract + verification evidence:
  `docs/ai/go-native-copilotkit.md` in `arda-be`.
* Gateway policy ids: `ai-agent-spike`, `ai-copilotkit-runtime`,
  `ai-conversations-read`, `ai-conversations-delete`, `ai-approvals-write`
  (all require permission `ai.assistant.use`). Gateway signs workload tokens
  with audience `ai-service`; `COPILOTKIT_RUNTIME_URL=http://ai-service:8080`.
* Model config lives in secret `arda-app-secrets` (`AI_MODEL_API_KEY`) plus
  Deployment env (`AI_MODEL_BASE_URL=https://opencode.ai/zen/v1`,
  `AI_MODEL_ID=x-preview-f-free`, `AI_ENABLE_AGENT=true`). Never commit keys.
* Deploy flow: push `arda-be` main -> GitHub Actions images -> ArgoCD image
  updater rewrites digests in `arda-infra` -> ArgoCD sync. A green CI build
  does NOT mean pods updated; compare the pinned `sha256:` digest on the
  Deployment first.

## 8. Windows / PowerShell gotchas (session-verified)

* **curl.exe JSON bodies**: PowerShell 5.1 strips embedded double quotes when
  passing inline args (`--data-raw '{"a":1}'` arrives as `{a:1}`). Write the
  body to a temp file and use `--data "@$env:TEMP\body.json"`. A 400
  `ai.invalid_copilotkit_envelope` with no server-side decode log = client-side
  quote mangling, not a backend bug.
* No `&&` chaining; use `cmd1; if ($?) { cmd2 }`. Avoid `-Encoding UTF8` on
  `Set-Content` for non-ASCII files (adds BOM/mojibake) - edit locale JSON via
  a small `node -e fs` script instead.
* Cluster access from this machine works directly via
  `KUBECONFIG=C:\Users\hoanv\AppData\Roaming\Freelens\kubeconfigs\<id>`;
  no SSH hop needed.
