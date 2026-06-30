# Arda Auth Stack

This folder deploys the Ory auth dependencies for the k3s dev cluster:

- Hydra: OAuth2/OIDC server.
- Kratos: identity, password login, and Kratos browser/API sessions.
- IAM app integration: `setup.sh` can patch the `iam-service` deployment with
  `SUPERADMIN_INITIAL_PASSWORD` so the app bootstraps the canonical superadmin.

The actual Arda authorization model lives in `iam-service`, not in Hydra or
Kratos. A working superadmin therefore needs both sides:

- Kratos identity: `superadmin@arda.local` can authenticate.
- IAM user: `superadmin@arda.local` has `SUPER_ADMIN` and wildcard permission.

`iam-service` creates/reconciles the IAM side on startup. This folder prepares
the Ory side and passes the bootstrap password to `iam-service`.

## Files

```text
auth/
  setup.sh
  values-hydra.yaml
  values-kratos.yaml
  identity.schema.json
  auth-admin-nodeport.yaml
  clients/arda-shell.json
```

## Defaults

```text
AUTH_NAMESPACE=auth
APP_NAMESPACE=platform
IAM_DEPLOYMENT=iam-service
SUPERADMIN_EMAIL=superadmin@arda.local
SUPERADMIN_INITIAL_PASSWORD=admin123
DEV_ADMIN_EMAIL=admin@arda.local
```

For dev, the script also seeds `admin@arda.local` with the same password because
older local IAM seed data still contains that account. For production-like
environments, set `SUPERADMIN_INITIAL_PASSWORD` from a real secret before
running the script.

## Usage

Deploy or reconcile auth:

```bash
cd arda-be/deployments/auth
chmod +x setup.sh
SUPERADMIN_INITIAL_PASSWORD=admin123 ./setup.sh deploy
```

Reset only Hydra/Kratos databases, then redeploy and reseed:

```bash
./setup.sh reset
```

Patch only the IAM deployment with the superadmin bootstrap secret:

```bash
SUPERADMIN_INITIAL_PASSWORD=admin123 ./setup.sh iam-secret
```

## What `setup.sh deploy` Does

1. Installs or upgrades Hydra and Kratos in namespace `auth`.
2. Creates the `arda-shell` Hydra OAuth client.
3. Imports Kratos seed identities:
   - `superadmin@arda.local`
   - `admin@arda.local`
4. Creates admin NodePorts:
   - Hydra Admin: `30445`
   - Kratos Admin: `30446`
5. If `deploy/iam-service` exists in namespace `platform`, creates/updates
   secret `iam-service-secrets`, sets env from it, and restarts `iam-service`.

After `iam-service` restarts, the expected dev logins are:

```text
superadmin@arda.local / admin123
admin@arda.local      / admin123
```

## Flow

```text
App
-> Hydra /oauth2/auth
-> /login?login_challenge=...
-> Login app uses Kratos login
-> auth-gateway accepts Hydra login/consent
-> /callback?code=...
-> auth-gateway session + IAM authorization context
```

## Checks

```bash
kubectl get pods -n auth
kubectl get svc -n auth | grep -E 'hydra|kratos'
kubectl get deploy -n platform iam-service
kubectl get secret -n platform iam-service-secrets
```

Check the Hydra client:

```bash
kubectl exec -n auth deploy/hydra -- \
  hydra get oauth2-client arda-shell \
  --endpoint http://127.0.0.1:4445
```
