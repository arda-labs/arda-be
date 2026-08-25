# Runtime evidence — 2026-08-25

This record was collected read-only from `k3s-node1` (`192.168.10.201`) through
the cluster's operator kubeconfig. It describes the currently deployed revision,
not the uncommitted refactor worktree.

## Cluster and platform

- `k3s-node1`, `k3s-node2` and `k3s-node3` are `Ready` on
  `v1.35.5+k3s1`.
- CloudNativePG `pg-main-1/2/3`, NATS, Garage, Valkey, Zeebe and Cloudflared
  pods were running at collection time.
- Argo CD reports `arda-frontend` `Synced/Healthy`, but `arda-backend`
  `Synced/Degraded`.
- No `Backup`/`ScheduledBackup` resources were returned for the `database`
  namespace, so automated CNPG backup evidence is not present.
- The NATS JetStream account exposed only `puchi-*` streams; no Arda
  notification stream/consumer was present in the live cluster.

## Current blockers in the deployed revision

1. All three HRM pods are `CreateContainerConfigError` because the live
   `arda-app/arda-app-secrets` Secret has no `HRM_DATABASE_DSN` key. This is a
   deployment-secret completeness failure, not an application fallback case.
2. One of four auth-gateway pods is in `CrashLoopBackOff` with exit code 139;
   the other three are running. The crash is not treated as proof of a source
   regression until the exact deployed image is reproduced and diagnosed.
3. No `arda-grpc-tls-*` Secrets exist in `arda-app`, and the live service
   Deployments do not contain the new `ARDA_GRPC_*` mounts or
   `ARDA_SERVICE_AUTH_SECRET` references. The mTLS/workload-identity source
   implementation is therefore not deployed.
4. The live IAM service returns `404` for `/metrics`, confirming that the
   observability middleware in the worktree is not in the running image.

## Edge and data evidence

From `k3s-node1`, unauthenticated requests to `/api/auth/me` and
`/api/admin/permissions?page=1&per_page=10` returned `401` with
`cf-cache-status: DYNAMIC` and Cloudflare `HKG` rays. Ten samples had edge
TTFB approximately `104–188 ms`; these are not authenticated application or DB
measurements.

The deployed auth boundary currently returns a different JSON `request_id` from
the `X-Request-Id` header for both routes. The worktree writer has the fix, but
rollout verification is still required.

The live database migration heads are still in July 2026; the August scope and
idempotency migrations in the worktree are not applied. Read-only counts of rows
with an unassigned/reserved tenant value (`NULL`, empty or `default`) were:

| Database | Tables affected | Rows requiring owner assignment |
| --- | --- | ---: |
| Finance | account classifications, accounts, internal accounts, journal definitions, process configs, regulatory accounts | 15 |
| CRM | customers | 12 |
| Workflow | business cases, role memberships | 15 |
| Notification | notifications, inbox, outbox, push subscriptions | 54 |
| Media | files, outbox events, upload sessions | 100 |
| Platform | file templates, lookup categories, organizations, system parameters | 28 |

These rows must be mapped by the data owner to an authoritative tenant before
`NOT VALID` constraints are validated. No automatic `default -> tenant` update
was performed.

## Gate disposition

Source/unit/static gates are green, including explicit Redis session-store
selection, fail-closed notification JetStream startup, Finance-to-Platform gRPC
startup checks, monorepo Compose build-context validation, FE credentials gates,
and infra manifest/backup/canary checks. Runtime mTLS, authenticated latency/load,
canary, automated backup/restore, event replay and tenant backfill gates remain
open until the new immutable artifacts are deployed and the above data/secret
issues are resolved with an auditable operator run.
