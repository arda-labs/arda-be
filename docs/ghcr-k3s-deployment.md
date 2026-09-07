# GHCR and k3s Deployment

Backend images are built by GitHub Actions and pushed to GHCR:

```text
ghcr.io/arda-labs/arda-be/auth-gateway:<tag>
ghcr.io/arda-labs/arda-be/ai-service:<tag>
ghcr.io/arda-labs/arda-be/iam-service:<tag>
ghcr.io/arda-labs/arda-be/platform-service:<tag>
ghcr.io/arda-labs/arda-be/finance-service:<tag>
ghcr.io/arda-labs/arda-be/media-service:<tag>
ghcr.io/arda-labs/arda-be/workflow-service:<tag>
ghcr.io/arda-labs/arda-be/crm-service:<tag>
ghcr.io/arda-labs/arda-be/notification-service:<tag>
ghcr.io/arda-labs/arda-be/hrm-service:<tag>
ghcr.io/arda-labs/arda-be/mdm-service:<tag>
ghcr.io/arda-labs/arda-be/loan-service:<tag>
ghcr.io/arda-labs/arda-be/deposit-service:<tag>
ghcr.io/arda-labs/arda-be/capital-service:<tag>
ghcr.io/arda-labs/arda-be/statistical-service:<tag>
```

Tags:

- `main` for pushes to the default branch.
- Git tag name for `v*` tags.
- Commit SHA for every workflow run.

## GitHub Settings

For the private repo, set Actions workflow permissions to:

- Contents: read
- Packages: write

The workflow uses `GITHUB_TOKEN` to push images for the same repo. No PAT is
needed in GitHub Actions.

## k3s Pull Secret

Create one PAT with only `read:packages`, then create pull secrets in app
namespaces:

```bash
kubectl -n arda-app create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io \
  --docker-username=<github-user> \
  --docker-password=<pat-with-read-packages> \
  --docker-email=<email>

kubectl -n arda-web create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io \
  --docker-username=<github-user> \
  --docker-password=<pat-with-read-packages> \
  --docker-email=<email>
```

## Deploy Backend

Kubernetes/Argo CD manifests and deployment bootstrap scripts live in the
sibling repo **`arda-infra`** (not in `arda-be`). Argo CD syncs from that repo;
do not `kubectl apply -k` from here.

Keep runtime secrets out of git:

```bash
kubectl -n arda-app create secret generic arda-app-secrets \
  --from-literal=IAM_DATABASE_DSN='<dsn>' \
  --from-literal=PLATFORM_DATABASE_DSN='<dsn>' \
  --from-literal=FINANCE_DATABASE_DSN='<dsn>' \
  --from-literal=MEDIA_DATABASE_DSN='<dsn>' \
  --from-literal=WORKFLOW_DATABASE_DSN='<dsn>' \
  --from-literal=CRM_DATABASE_DSN='<dsn>' \
  --from-literal=HRM_DATABASE_DSN='<dsn>' \
  --from-literal=NOTIFICATION_DATABASE_DSN='<dsn>' \
  --from-literal=MDM_DATABASE_DSN='<dsn>' \
  --from-literal=LOAN_DATABASE_DSN='<dsn>' \
  --from-literal=DEPOSIT_DATABASE_DSN='<dsn>' \
  --from-literal=CAPITAL_DATABASE_DSN='<dsn>' \
  --from-literal=STATISTICAL_DATABASE_DSN='<dsn>' \
  --from-literal=ARDA_SERVICE_AUTH_SECRET='<random-workload-secret>' \
  --from-literal=INTROSPECTION_CLIENT_ID='<oauth-client-id>' \
  --from-literal=INTROSPECTION_CLIENT_SECRET='<oauth-client-secret>' \
  --from-literal=REDIS_URL='<redis-url>' \
  --from-literal=NATS_URL='<nats-url>' \
  --from-literal=GARAGE_ACCESS_KEY='<garage-access-key>' \
  --from-literal=GARAGE_SECRET_KEY='<garage-secret-key>'
```

For an existing cluster, patch the missing keys instead of recreating the
secret:

```bash
kubectl -n arda-app patch secret arda-app-secrets --type merge -p '{
  "stringData": {
    "FINANCE_DATABASE_DSN": "<database-dsn-from-secret-manager>",
    "MEDIA_DATABASE_DSN": "<database-dsn-from-secret-manager>",
    "WORKFLOW_DATABASE_DSN": "<database-dsn-from-secret-manager>",
    "CRM_DATABASE_DSN": "<database-dsn-from-secret-manager>",
    "HRM_DATABASE_DSN": "<database-dsn-from-secret-manager>",
    "NOTIFICATION_DATABASE_DSN": "<database-dsn-from-secret-manager>",
    "ARDA_SERVICE_AUTH_SECRET": "<random-workload-secret>",
    "INTROSPECTION_CLIENT_ID": "<oauth-client-id>",
    "INTROSPECTION_CLIENT_SECRET": "<oauth-client-secret>",
    "GARAGE_ACCESS_KEY": "<garage-access-key>",
    "GARAGE_SECRET_KEY": "<garage-secret-key>"
  }
}'
```

The databases and roles must exist before the services start. The services run
their own schema migrations on startup. Database bootstrap is handled by the
scripts under `arda-infra/database/`.
