# PostgreSQL integration tests

The loan and finance service smoke tests run against a fresh database per test.
Set `ARDA_TEST_DSN` to a PostgreSQL 18 test server. The supplied role needs
permission to create databases and must own the databases it creates. The test
helper generates a random `arda_test_<hex>` database, applies that service's
embedded Goose migrations, and drops the database after the test. It does not
run migrations against the database named in the DSN.

Use a disposable PostgreSQL server only. Never point `ARDA_TEST_DSN` at
production or a shared development database. The database server needs enough
space and a role with `CREATEDB`; the role must also be allowed to connect to
the initial database in the DSN. PostgreSQL 18 is required because service
migrations use the built-in `uuidv7()` function.

## Windows PowerShell

Store the DSN in a user-only secret file, then load it into the current process:

```powershell
$env:ARDA_TEST_DSN = (Get-Content -Raw "$env:USERPROFILE\.secrets\arda-test-dsn").Trim()
```

Run the DB-backed smoke suites from each service directory:

```powershell
Set-Location D:\Github\arda-labs\arda-be\apps\loan-service
go test ./internal/service -run 'Test(AccrualSmoke|AdjustmentResolveSmoke|CollectionSmoke|ContractUpdateSmoke|DisbursementSmoke|ProvisionSmoke)$' -count=1 -v

Set-Location D:\Github\arda-labs\arda-be\apps\finance-service
go test ./internal/service -run 'Test(PostingSmoke|ReportingSmoke|TwoPhaseBalanceSmoke)$' -count=1 -v

Remove-Item Env:ARDA_TEST_DSN
```

Without `ARDA_TEST_DSN`, the database smoke tests skip with an explicit message;
the non-database test suite remains runnable. If setup fails, inspect the
sanitized error for connection, `CREATEDB`, or PostgreSQL version issues. The
test helper never prints the DSN.

## Linux/macOS

```sh
export ARDA_TEST_DSN="$(cat /run/secrets/arda-test-dsn)"
(cd apps/loan-service && go test ./internal/service -run 'Test(AccrualSmoke|AdjustmentResolveSmoke|CollectionSmoke|ContractUpdateSmoke|DisbursementSmoke|ProvisionSmoke)$' -count=1 -v)
(cd apps/finance-service && go test ./internal/service -run 'Test(PostingSmoke|ReportingSmoke|TwoPhaseBalanceSmoke)$' -count=1 -v)
unset ARDA_TEST_DSN
```

The `arda-postgres/testdb` helper creates and drops one isolated database for
each test; do not run these tests with a DSN to a server where database creation
is not disposable. The deposit service currently has no DB-gated `t.Skip`
tests, so this task does not add test DB setup there.
