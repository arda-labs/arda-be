#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${AUTH_NAMESPACE:-auth}"
APP_NAMESPACE="${APP_NAMESPACE:-arda-app}"
IAM_DEPLOYMENT="${IAM_DEPLOYMENT:-iam-service}"
IAM_SECRET="${IAM_SECRET:-iam-service-secrets}"
HYDRA_RELEASE="${HYDRA_RELEASE:-hydra}"
KRATOS_RELEASE="${KRATOS_RELEASE:-kratos}"
ORY_REPO_NAME="${ORY_REPO_NAME:-ory}"
ORY_REPO_URL="${ORY_REPO_URL:-https://k8s.ory.sh/helm/charts}"

SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL:-superadmin@arda.local}"
SUPERADMIN_PASSWORD="${SUPERADMIN_INITIAL_PASSWORD:-${SUPERADMIN_PASSWORD:-admin123}}"
DEV_ADMIN_EMAIL="${DEV_ADMIN_EMAIL:-admin@arda.local}"

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
  echo "=== Reset Ory databases ==="
  kubectl run psql-client --rm -i --restart=Never \
    --image postgres:16-alpine \
    --namespace "$NAMESPACE" \
    --env="PGPASSWORD=123456" \
    --command -- sh -c '
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "DO \$do\$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = \$q\$arda_hydra\$q\$) THEN CREATE ROLE arda_hydra LOGIN PASSWORD \$q\$123456\$q\$; END IF; END \$do\$;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "DO \$do\$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = \$q\$arda_kratos\$q\$) THEN CREATE ROLE arda_kratos LOGIN PASSWORD \$q\$123456\$q\$; END IF; END \$do\$;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname IN (\$q\$hydra\$q\$, \$q\$kratos\$q\$) AND pid <> pg_backend_pid();" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "DROP DATABASE IF EXISTS hydra;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "DROP DATABASE IF EXISTS kratos;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "CREATE DATABASE hydra OWNER arda_hydra;" 2>/dev/null || true
      psql -h pg-main-rw.database.svc.cluster.local -U postgres \
        -c "CREATE DATABASE kratos OWNER arda_kratos;" 2>/dev/null || true
      echo "Ory databases reset done."
    ' 2>/dev/null || true
}

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
  kubectl exec -n "$NAMESPACE" "deploy/$HYDRA_RELEASE" -- \
    hydra delete oauth2-client arda-shell \
    --endpoint http://127.0.0.1:4445 2>/dev/null || true

  kubectl exec -n "$NAMESPACE" "deploy/$HYDRA_RELEASE" -- \
    hydra create oauth2-client \
    --endpoint http://127.0.0.1:4445 \
    --id arda-shell \
    --grant-type authorization_code \
    --grant-type refresh_token \
    --response-type code \
    --scope openid,email,offline_access \
    --redirect-uri https://arda.io.vn/callback \
    --token-endpoint-auth-method none
}

create_kratos_identity() {
  local email="$1"
  local name="$2"

  kubectl exec -n "$NAMESPACE" "deploy/$KRATOS_RELEASE" -- \
    kratos import \
    --endpoint http://127.0.0.1:4434 \
    /dev/stdin <<EOF 2>/dev/null || true
{
  "schema_id": "default",
  "traits": {
    "email": "$email",
    "name": "$name"
  },
  "credentials": {
    "password": {
      "config": {
        "password": "$SUPERADMIN_PASSWORD"
      }
    }
  }
}
EOF
}

create_seed_identities() {
  echo "=== Create Kratos seed identities ==="
  create_kratos_identity "$SUPERADMIN_EMAIL" "Super Admin"
  create_kratos_identity "$DEV_ADMIN_EMAIL" "Admin"
}

configure_iam_superadmin() {
  echo "=== Configure IAM superadmin bootstrap secret ==="
  if ! kubectl get deployment "$IAM_DEPLOYMENT" -n "$APP_NAMESPACE" >/dev/null 2>&1; then
    echo "IAM deployment $APP_NAMESPACE/$IAM_DEPLOYMENT not found; skipping IAM env patch."
    echo "Run after deploying iam-service:"
    echo "  kubectl -n $APP_NAMESPACE create secret generic $IAM_SECRET --from-literal=SUPERADMIN_INITIAL_PASSWORD=<secret> --dry-run=client -o yaml | kubectl apply -f -"
    echo "  kubectl -n $APP_NAMESPACE set env deploy/$IAM_DEPLOYMENT --from=secret/$IAM_SECRET"
    echo "  kubectl -n $APP_NAMESPACE rollout restart deploy/$IAM_DEPLOYMENT"
    return 0
  fi

  kubectl -n "$APP_NAMESPACE" create secret generic "$IAM_SECRET" \
    --from-literal=SUPERADMIN_INITIAL_PASSWORD="$SUPERADMIN_PASSWORD" \
    --dry-run=client -o yaml | kubectl apply -f -

  kubectl -n "$APP_NAMESPACE" set env "deploy/$IAM_DEPLOYMENT" \
    --from="secret/$IAM_SECRET"

  kubectl -n "$APP_NAMESPACE" rollout restart "deploy/$IAM_DEPLOYMENT"
}

setup_nodeports() {
  echo "=== Setup NodePorts ==="
  kubectl apply -n "$NAMESPACE" -f auth-admin-nodeport.yaml
}

case "${1:-deploy}" in
  reset)
    reset_databases
    deploy_hydra
    deploy_kratos
    create_hydra_client
    create_seed_identities
    setup_nodeports
    configure_iam_superadmin
    ;;
  deploy)
    deploy_hydra
    deploy_kratos
    create_hydra_client
    create_seed_identities
    setup_nodeports
    configure_iam_superadmin
    ;;
  reset-db)
    reset_databases
    ;;
  client)
    create_hydra_client
    create_seed_identities
    ;;
  nodeport)
    setup_nodeports
    ;;
  iam-secret)
    configure_iam_superadmin
    ;;
  *)
    echo "Usage: $0 {deploy|reset|reset-db|client|nodeport|iam-secret}"
    exit 1
    ;;
esac

echo "=== Done ==="
echo "Hydra public:  https://auth.arda.io.vn"
echo "Kratos public: https://identity.arda.io.vn"
echo "Hydra Admin NodePort: 30445"
echo "Kratos Admin NodePort: 30446"
echo "Superadmin login: $SUPERADMIN_EMAIL / $SUPERADMIN_PASSWORD"
echo "Dev admin login:  $DEV_ADMIN_EMAIL / $SUPERADMIN_PASSWORD"
