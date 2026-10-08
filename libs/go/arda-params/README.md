# arda-params

`arda-params` provides one typed, effective-dated parameter resolver for Go services. A service builds a `ModuleSpec`, declares it with `Registry.Declare`, and calls `Registry.Verify(ctx)` after migrations and before opening listeners. Required configuration is fail-fast; `Get[T]` has no implicit default. `ParamSpec.Scope` is the most specific permitted scope: ORG permits ORG/TENANT/GLOBAL resolution, TENANT permits TENANT/GLOBAL, and GLOBAL permits only GLOBAL.

Go does not support generic methods, so the typed API is the package function `params.Get[T](ctx, registry, module, code, scopeKey)`. The resolver applies ORG > TENANT > GLOBAL, filters by `effective_from <= EffectiveDate` and inclusive `effective_to >= EffectiveDate`, and requires a non-zero effective date. A GLOBAL row has no tenant or org; tenant/org rows require a tenant, and ORG additionally requires a non-empty org code. No `%` wildcard is used.

`RenderGoConstants` deterministically renders string constants from declared `CodeSetSpec` items. Modules should keep the code-set declaration as the source input to generation and commit the generated Go file alongside it.

The host service owns the migration and database connection. The sample migration is in `apps/loan-service/migrations/20261008150000_loan_parameter_registry.sql`; it moves the legacy general-provision fallback `0.7500` to an all-history GLOBAL parameter. Because that legacy table has no tenant or effective-date ownership, the migration aborts if it finds any org-specific row rather than guessing its scope.
