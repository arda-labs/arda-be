# Deployment Namespace Layout

Arda uses one k3s environment. Namespaces separate ownership boundaries, not
deployment stages.

## Namespaces

| Namespace | Owns | Notes |
| --- | --- | --- |
| `database` | PostgreSQL/operator and database services | Keep DB credentials and operator RBAC scoped here. |
| `platform` | Shared infrastructure: NATS, Garage, Valkey, Zeebe | No application services should run here. |
| `auth` | Ory Hydra and Ory Kratos | Auth dependencies only; Arda IAM still runs as an app service. |
| `arda-app` | Backend services: `auth-gateway`, `iam-service`, `platform-service`, business services | Default namespace for Go services. |
| `arda-web` | Frontend web app, if deployed on k3s | Keep web ingress/secrets separate from backend service secrets. |

## Rules

- Put new backend services in `arda-app` by default.
- Put frontend workloads in `arda-web`.
- Keep shared brokers/storage/workflow engines in `platform`.
- Keep Hydra/Kratos in `auth`; do not put IAM there because IAM is an Arda app
  service and owns authorization/business identity data.
- Do not create one namespace per microservice yet. Split a service out only
  when it has materially different RBAC, network, secret, or operational needs.

## Required App Namespace Setup

```bash
kubectl create namespace arda-app --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace arda-web --dry-run=client -o yaml | kubectl apply -f -
```

The auth setup script expects `iam-service` in `arda-app` by default:

```bash
cd arda-be/deployments/auth
SUPERADMIN_INITIAL_PASSWORD=admin123 ./setup.sh iam-secret
```

Override only when a deployment is intentionally elsewhere:

```bash
APP_NAMESPACE=<namespace> ./setup.sh iam-secret
```
