# PostgreSQL RLS Feasibility Pilot

Status: source-side pilot design; production adoption is not approved.

## Decision boundary

Application repository predicates remain the primary tenant-isolation control.
RLS is defense in depth only after every transaction that touches a tenant-owned
table establishes `arda.tenant_id` with `SET LOCAL`. Enabling RLS before that
condition is true would turn missing context into broad outages, while disabling
the policy on a pooled connection would risk cross-request context leakage.

The pilot therefore uses an isolated scratch table and never changes an
application table or production policy.

## Scratch policy

Run the following only in a disposable PostgreSQL database:

```sql
CREATE TABLE rls_probe (
  id uuid PRIMARY KEY,
  tenant_id text NOT NULL,
  value text NOT NULL
);

ALTER TABLE rls_probe ENABLE ROW LEVEL SECURITY;
ALTER TABLE rls_probe FORCE ROW LEVEL SECURITY;

CREATE POLICY rls_probe_tenant_isolation ON rls_probe
  USING (tenant_id = current_setting('arda.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('arda.tenant_id', true));
```

Each request-scoped transaction must set the value and then query:

```sql
BEGIN;
SELECT set_config('arda.tenant_id', 'tenant-a', true);
SELECT id, tenant_id, value FROM rls_probe;
COMMIT;
```

`current_setting(..., true)` returning NULL must yield no rows and reject
inserts through `WITH CHECK`; the application must still reject an absent
tenant before reaching the database.

## Required evidence before adoption

1. Same-tenant read/write succeeds.
2. Cross-tenant read returns zero rows and cross-tenant write is rejected.
3. Missing tenant context cannot read or write.
4. A pooled connection does not retain tenant context after commit.
5. Rollback and cancellation clear the transaction-local setting.
6. Repository query tests and real-PostgreSQL integration tests remain green.
7. p95 overhead and lock/deadlock impact are measured for representative list
   and command paths.

Until all seven results are attached to the runtime evidence ledger, do not
enable RLS on IAM, CRM, HRM, Finance, Workflow, Platform, Media or Notification
tables. Do not map the historical `default` tenant to a real tenant as part of
this pilot.

