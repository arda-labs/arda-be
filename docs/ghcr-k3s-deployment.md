# GHCR and k3s Deployment

Backend images are built by GitHub Actions and pushed to GHCR:

```text
ghcr.io/arda-labs/arda-be/auth-gateway:<tag>
ghcr.io/arda-labs/arda-be/iam-service:<tag>
ghcr.io/arda-labs/arda-be/platform-service:<tag>
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

Replace `arda-labs` in manifests once:

```bash
rg -l "arda-labs" deployments/k8s | xargs sed -i "s/arda-labs/<github-owner-or-org>/g"
```

Then apply:

```bash
kubectl apply -k deployments/k8s/apps
```

Keep runtime secrets out of git:

```bash
kubectl -n arda-app create secret generic arda-app-secrets \
  --from-literal=IAM_DATABASE_DSN='<dsn>' \
  --from-literal=PLATFORM_DATABASE_DSN='<dsn>' \
  --from-literal=REDIS_URL='<redis-url>' \
  --from-literal=NATS_URL='<nats-url>'
```
