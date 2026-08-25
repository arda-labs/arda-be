# Internal gRPC mTLS runbook

The backend requires encrypted internal gRPC transport. Clients and servers
load all three files below at startup; there is no plaintext or `insecure`
fallback:

```text
ARDA_GRPC_CA_FILE=/var/run/arda-grpc-tls/ca.crt
ARDA_GRPC_CERT_FILE=/var/run/arda-grpc-tls/tls.crt
ARDA_GRPC_KEY_FILE=/var/run/arda-grpc-tls/tls.key
```

`ARDA_SERVICE_AUTH_SECRET` remains required separately. mTLS proves possession
of an issued workload certificate; the HMAC assertion supplies the source
service and destination audience policy.

## Certificate contract

Use one private CA for the internal gRPC trust domain and issue one certificate
and private key per workload. Every certificate must have `clientAuth` and
`serverAuth` extended key usages, plus DNS SANs matching the destination names
used by clients (`crm-service`, `iam-service`, `platform-service`, and
`workflow-service`) and the corresponding cluster DNS names such as
`crm-service.arda-app.svc.cluster.local`.

Do not commit CA keys, workload keys, certificates, or generated files.

## Local Compose

Generate a local CA and workload certificates under
`arda-be/.dev/grpc-tls/{crm,iam,platform,workflow,hrm,finance}`. Each directory
must contain `ca.crt`, `tls.crt`, and `tls.key`. The Compose mounts are
read-only. A caller uses its own workload certificate; it does not reuse the
destination certificate.

Use an approved internal certificate tool or OpenSSL script to issue these
files, then verify before starting:

```powershell
Get-ChildItem .dev/grpc-tls/*/ca.crt,
  .dev/grpc-tls/*/tls.crt,
  .dev/grpc-tls/*/tls.key
docker compose config
```

Never put private keys in `.env`, an image layer, or a ConfigMap.

## Kubernetes

Create one Secret per workload in `arda-app` with exactly the keys `ca.crt`,
`tls.crt`, and `tls.key`:

```text
arda-grpc-tls-crm
arda-grpc-tls-iam
arda-grpc-tls-platform
arda-grpc-tls-workflow
arda-grpc-tls-hrm
arda-grpc-tls-finance
```

For example, from a protected operator workstation:

```bash
kubectl -n arda-app create secret generic arda-grpc-tls-crm \
  --from-file=ca.crt=ca.crt \
  --from-file=tls.crt=crm-service.crt \
  --from-file=tls.key=crm-service.key
```

Repeat after reviewing each certificate's SANs. Keep generated Secret YAML out
of Git and manage it through the approved secret-management system.

Before rollout, verify every Deployment has the matching read-only mount and
all three file environment variables. Restart one workload at a time and
check readiness plus a real gRPC call from each allowed caller.

## Rotation and incident response

For workload rotation, issue a replacement from the same CA, update that
workload's Secret, restart only that workload, and record serial/expiry/owner
in the deployment ledger. For CA rotation, distribute an old+new CA bundle,
rotate workload certificates, then remove the old CA in a second rollout.

Classify failures before changing policy: TLS handshake/SAN/expiry failures
are certificate issues; `Unauthenticated` is assertion/secret; and
`PermissionDenied` is source allowlist or audience policy. Do not restore
plaintext transport to recover a deployment.
