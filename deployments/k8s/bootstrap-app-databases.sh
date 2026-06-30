#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${DB_NAMESPACE:-database}"
HOST="${DB_HOST:-pg-main-rw.database.svc.cluster.local}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-123456}"

kubectl run arda-app-db-bootstrap --rm -i --restart=Never \
  --image postgres:16-alpine \
  --namespace "$NAMESPACE" \
  --env="DB_HOST=$HOST" \
  --env="PGPASSWORD=$POSTGRES_PASSWORD" \
  --command -- sh -s <<'EOF'
set -eu
for item in \
  arda_finance:finance \
  arda_media:media \
  arda_workflow:workflow \
  arda_crm:crm \
  arda_notification:notification
do
  role="${item%%:*}"
  db="${item##*:}"
  psql -h "$DB_HOST" -U postgres -v ON_ERROR_STOP=1 \
    -c "DO \$do\$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$role') THEN EXECUTE format('CREATE ROLE %I LOGIN PASSWORD %L', '$role', '123456'); END IF; END \$do\$;"
  psql -h "$DB_HOST" -U postgres -v ON_ERROR_STOP=1 \
    -tc "SELECT 1 FROM pg_database WHERE datname = '$db'" | grep -q 1 || \
    psql -h "$DB_HOST" -U postgres -v ON_ERROR_STOP=1 \
      -c "CREATE DATABASE $db OWNER $role;"
done
EOF
