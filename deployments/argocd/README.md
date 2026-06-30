# Argo CD

Argo CD owns deployment sync for Arda app manifests:

```text
GitHub Actions -> build images -> push GHCR
Argo CD        -> sync manifests from Git -> k3s
```

Argo CD does not build images. It only reconciles Kubernetes manifests.

## Install Argo CD

```bash
kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
kubectl -n argocd rollout status deploy/argocd-server
```

Get the initial admin password:

```bash
kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath="{.data.password}" | base64 -d
```

Port-forward the UI:

```bash
kubectl -n argocd port-forward svc/argocd-server 8080:443
```

Open `https://localhost:8080`.

## Private GitHub Repo Access

Create a GitHub PAT with the minimum repo read access needed for private repos.
For classic tokens this usually means `repo`. For fine-grained tokens, grant
read-only `Contents` access to `arda-be` and `arda-fe`.

Create Argo CD repo credentials:

```bash
kubectl -n argocd create secret generic arda-github-repo-creds \
  --from-literal=type=git \
  --from-literal=url=https://github.com/arda-labs \
  --from-literal=username=<github-user> \
  --from-literal=password=<github-pat> \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n argocd label secret arda-github-repo-creds \
  argocd.argoproj.io/secret-type=repo-creds --overwrite
```

## Apply Arda Applications

```bash
cd arda-be
kubectl apply -k deployments/argocd
```

This creates:

- `AppProject/arda`
- `Application/arda-backend`
- `Application/arda-frontend`

## Required Runtime Secrets

Argo CD will create app resources, but runtime secrets are still cluster-local.
Create them before syncing apps:

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

Also create `arda-app-secrets` and `iam-service-secrets` as described in
`../../docs/ghcr-k3s-deployment.md`.

## Image Tags

The current manifests use the `main` tag for simplicity. After GitHub Actions
pushes a new `main` image, restart the deployment or later add Argo CD Image
Updater / SHA-tag manifest updates.
