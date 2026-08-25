# Problem details catalog

This directory is the source catalog for the stable `type` URLs returned by
Arda HTTP APIs. The runtime currently emits URLs such as
`https://docs.arda.io.vn/problems/insufficient_permissions`; the documentation
site should publish a page for every entry here.

## Required page contract

Every problem page must define:

| Field | Rule |
| --- | --- |
| `code` | Stable machine-readable identifier. Never use a translated message. |
| HTTP status | The status clients should branch on. |
| `type` | Canonical documentation URL emitted in `application/problem+json`. |
| Meaning | What the server rejected, in one sentence. |
| Client action | Whether to retry, re-authenticate, select a tenant, or show access denied. |
| Operator action | Log fields and checks needed to diagnose the problem. |
| Example | A complete redacted response with `request_id`. |
| Security notes | Data that must not be exposed or trusted from the browser. |
| Related routes | The route families that can emit the problem. |

Problem pages are operational contracts, not API implementation notes. Error
messages may be localized or improved without changing the `code` or `type`.

## Initial catalog

- [insufficient_permissions](insufficient_permissions.md) — authenticated
  actor lacks the required tenant or global capability.
- [auth.error.unauthorized](auth.error.unauthorized.md) — no valid
  authenticated session is available.
- [auth.error.forbidden](auth.error.forbidden.md) — authenticated request is
  denied by the generic auth policy.
- [tenant_context_unavailable](tenant_context_unavailable.md) — the verified
  actor has no usable active tenant context.
- [organization_forbidden](organization_forbidden.md) — the requested
  organization is outside the active tenant context.
- [user_context_unavailable](user_context_unavailable.md) — IAM could not
  provide a current user authorization context.

## Publishing plan for `docs.arda.io.vn`

There is no documentation-site source in the current workspace, so these
Markdown files are the canonical content until that site is added. The future
site should:

1. Build this catalog as static routes at `/problems/{code}`.
2. Keep the URL slug equal to the stable `code` for backwards compatibility.
3. Render a controlled 404 page for unknown codes; never expose stack traces.
4. Add a CI check that extracts every `type` URL from backend code/OpenAPI and
   verifies that a matching catalog page exists.
5. Publish an OpenAPI link and a short client-handling section on each page.
6. Treat deletion or renaming of a problem URL as a versioned API change.

When the docs site is created, it should consume this directory or a generated
equivalent rather than duplicating problem definitions by hand.
