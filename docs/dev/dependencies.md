# Dev Dependencies

Last updated: 2026-10-12

Local development connects to services running in the k3s LAN cluster. The
committed `apps/*/configs/config.yaml` files and Go defaults contain no LAN
addresses: they point at `localhost` and match the port-forwards below.

## Port-forward (default)

```powershell
kubectl -n auth port-forward svc/hydra-admin 4445:4445
kubectl -n auth port-forward svc/kratos-admin 4434:80
kubectl -n platform port-forward svc/nats 4222:4222
kubectl -n platform port-forward svc/zeebe-zeebe-gateway 26500:26500 18080:8080
```

| Dependency | Default | Env override |
| --- | --- | --- |
| Hydra admin (auth-gateway, iam-service) | `http://localhost:4445` | `HYDRA_ADMIN_URL` |
| Kratos admin (iam-service) | `http://localhost:4434` | `KRATOS_ADMIN_URL` |
| NATS (media, notification, finance) | `nats://localhost:4222` | `NATS_URL` |
| Zeebe gRPC (workflow-service) | `localhost:26500` | `ZEEBE_ADDR` |
| Zeebe REST (workflow-service) | `http://localhost:18080` | `ZEEBE_REST_ADDR` |
| Hydra public | `https://auth.arda.io.vn` | `HYDRA_PUBLIC_URL` |
| Kratos public | `https://identity.arda.io.vn` | `KRATOS_PUBLIC_URL` |

Zeebe REST is forwarded to 18080 so it does not collide with iam-service on
8080 under `scripts/dev.ps1`.

## LAN NodePorts

Node IPs and NodePort endpoints (PostgreSQL, Valkey, NATS, Hydra/Kratos admin)
live in `arda-infra/dev/endpoints.example.env`. To use them instead of
port-forwards with `scripts/dev.ps1`, set the overrides above in `.env.local`.

`scripts/check-dev-deps.ps1` probes the NodePorts on `-Node <ip>`, or on
`$env:K3S_NODE_1` when `-Node` is omitted.

## Common Local Env

Copy `.env.example` to `.env` for docker compose and fill secrets locally.
`scripts/dev.ps1` (bare metal) loads `.env.local` instead, without overriding
variables already set in the shell; `.env` stays compose-only because its
values target containers. Both files are gitignored; do not commit real
secrets.

