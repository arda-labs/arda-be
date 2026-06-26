#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="auth"
HYDRA_RELEASE="hydra"
KRATOS_RELEASE="kratos"
ORY_REPO_NAME="ory"
ORY_REPO_URL="https://k8s.ory.sh/helm/charts"

cd "$(dirname "$0")"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing command: $1"
    exit 1
  }
}

require_cmd kubectl
require_cmd helm

kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 || kubectl create namespace "$NAMESPACE"

helm repo add "$ORY_REPO_NAME" "$ORY_REPO_URL" >/dev/null 2>&1 || true
helm repo update

reset_databases() {
  echo "=== Reset databases ==="
  kubectl run psql-client --rm -i --restart=Never \
    --image postgres:16-alpine \
    --namespace "$NAMESPACE" \
    --env="PGPASSWORD=123456" \
    --command -- sh -c '
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "DROP DATABASE IF EXISTS hydra;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "DROP DATABASE IF EXISTS kratos;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "CREATE DATABASE hydra;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "CREATE DATABASE kratos;" 2>/dev/null || true
      echo "Databases reset done."
    ' 2>/dev/null || true
}

# Secrets cố định (không sinh random)
# Hydra: system=kW7XpL9mR4vB2nJ8qT5yF3cA1gH6sD0e cookie=mN5bV8cX2zL1pK6jR4tY9wQ3aH7uE0s
# Kratos: default=kratos-default-secret-arda-2026-fixed-key cookie=kratos-cookie-secret-arda-2026-fixed-key

deploy_hydra() {
  echo "=== Deploy Hydra ==="
  helm upgrade --install "$HYDRA_RELEASE" "$ORY_REPO_NAME/hydra" \
    -n "$NAMESPACE" \
    -f values-hydra.yaml
  kubectl rollout status deployment "$HYDRA_RELEASE" -n "$NAMESPACE" --timeout=120s
}

deploy_kratos() {
  echo "=== Deploy Kratos ==="
  helm upgrade --install "$KRATOS_RELEASE" "$ORY_REPO_NAME/kratos" \
    -n "$NAMESPACE" \
    -f values-kratos.yaml
  kubectl rollout status deployment "$KRATOS_RELEASE" -n "$NAMESPACE" --timeout=120s
}

create_hydra_client() {
  echo "=== Create Hydra OAuth2 client ==="
  kubectl exec -n "$NAMESPACE" deploy/$HYDRA_RELEASE -- \
    hydra delete oauth2-client arda-shell \
    --endpoint http://127.0.0.1:4445 2>/dev/null || true

  kubectl exec -n "$NAMESPACE" deploy/$HYDRA_RELEASE -- \
    hydra create oauth2-client \
    --endpoint http://127.0.0.1:4445 \
    --id arda-shell \
    --grant-type authorization_code \
    --grant-type refresh_token \
    --response-type code \
    --scope openid,email,offline_access \
    --redirect-uri http://localhost:5000/callback \
    --redirect-uri https://arda.io.vn/callback \
    --token-endpoint-auth-method none

  echo "=== Create admin identity in Kratos ==="
  kubectl exec -n "$NAMESPACE" deploy/$KRATOS_RELEASE -- \
    kratos import \
    --endpoint http://127.0.0.1:4434 \
    /dev/stdin << 'EOF' 2>/dev/null || true
{
  "schema_id": "default",
  "traits": {
    "email": "admin@arda.local",
    "name": "Admin"
  },
  "credentials": {
    "password": {
      "config": {
        "password": "admin123"
      }
    }
  }
}
EOF
}

setup_nodeports() {
  echo "=== Setup NodePorts ==="
  # Hydra Admin → 30445
  cat << 'YAML' | kubectl apply -n "$NAMESPACE" -f -
apiVersion: v1
kind: Service
metadata:
  name: hydra-admin-nodeport
spec:
  type: NodePort
  selector:
    app.kubernetes.io/name: hydra
    app.kubernetes.io/instance: hydra
  ports:
    - port: 4445
      targetPort: 4445
      nodePort: 30445
YAML

  # Kratos Admin → 30446
  cat << 'YAML' | kubectl apply -n "$NAMESPACE" -f -
apiVersion: v1
kind: Service
metadata:
  name: kratos-admin-nodeport
spec:
  type: NodePort
  selector:
    app.kubernetes.io/name: kratos
    app.kubernetes.io/instance: kratos
  ports:
    - port: 4434
      targetPort: 4434
      nodePort: 30446
YAML
}

# ── Main ──
case "${1:-deploy}" in
  reset)
    reset_databases
    deploy_hydra
    deploy_kratos
    create_hydra_client
    setup_nodeports
    ;;
  deploy)
    deploy_hydra
    deploy_kratos
    create_hydra_client
    setup_nodeports
    ;;
  reset-db)
    reset_databases
    ;;
  client)
    create_hydra_client
    ;;
  nodeport)
    setup_nodeports
    ;;
  *)
    echo "Usage: $0 {deploy|reset|reset-db|client|nodeport}"
    exit 1
    ;;
esac

echo "=== Done ==="
echo "Hydra public:  https://auth.arda.io.vn"
echo "Kratos public: https://identity.arda.io.vn"
echo "Hydra Admin NodePort: 30445"
echo "Admin login: admin@arda.local / admin123"
