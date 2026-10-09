# arda-params

`arda-params` provides a typed, effective-dated parameter resolver backed by `platform-service`. A service builds a `ModuleSpec`, declares it with `Registry.Declare`, and calls `Registry.Verify(ctx)` before opening listeners. Required configuration is fail-fast; `Get[T]` has no implicit default. Successful resolutions are cached for 30 seconds. `ParamSpec.Scope` is the most specific permitted scope: ORG permits ORG/TENANT/GLOBAL resolution, TENANT permits TENANT/GLOBAL, and GLOBAL permits only GLOBAL.

Go does not support generic methods, so the typed API is the package function `params.Get[T](ctx, registry, module, code, scopeKey)`. The resolver applies ORG > TENANT > GLOBAL, filters by `effective_from <= EffectiveDate` and inclusive `effective_to >= EffectiveDate`, and requires a non-zero effective date. A GLOBAL row has no tenant or org; tenant/org rows require a tenant, and ORG additionally requires a non-empty org code. No `%` wildcard is used.

`RenderGoConstants` deterministically renders string constants from declared `CodeSetSpec` items. Catalog verification uses Platform lookup RPCs; each `CodeSetSpec` must declare its tenant scope, and lookup metadata may carry `parent_code` for hierarchy validation.

`platform-service` owns parameter migrations and storage. `apps/platform-service/migrations/20261008160000_parameter_effective_dates.sql` adds module, unit, and effective-date fields. Loan-service must resolve parameters over its authenticated Platform RPC client; it must not read parameter tables directly.

For the legacy general-provision setting, run `go run ./apps/loan-service/cmd/transfer-provision-rate` once after the Platform migration is applied and before switching loan-service to the new build. Set `DATABASE_DSN` to the loan database and `PLATFORM_DATABASE_DSN` to the Platform database. The command copies the exact legacy `%` value as an all-history GLOBAL parameter, verifies it, and safely accepts a repeat run. It refuses unmapped org-specific source rows or conflicting Platform values. Keep the legacy loan rate row until the transfer readback is confirmed.
