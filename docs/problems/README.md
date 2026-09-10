# Problem details catalog

This directory is the source catalog for the stable `type` URLs returned by
Arda HTTP APIs (`https://docs.arda.io.vn/problems/<code>`; the URL prefix is
the `ardahttp.ProblemsTypeBaseURL` constant in
`libs/go/arda-http/response.go`). The documentation site is generated from
this directory by `scripts/build-problem-docs.mjs` into
`cloudflare/docs/` and served by the `arda-problem-docs` Worker — see
[`cloudflare/docs/README.md`](../../cloudflare/docs/README.md).

## Page format

Every problem page is a Markdown file named `<code>.md` with a front-matter
block using a small YAML subset (flat keys, `|` block scalars, simple lists):

```markdown
---
code: auth.error.unauthorized
status: 401
title: Authentication required
summary: |
  One-sentence meaning of the problem.
client_action: |
  What the client should do (retry policy, re-auth, tenant switch…).
operator_action: |
  Log fields and checks needed to diagnose the problem.
related_routes:
  - Route families that can emit this problem
---

Optional extra prose rendered at the bottom of the page.
```

Rules enforced by `scripts/check-problem-catalog.mjs`:

- `code`, `status`, `title`, `summary` are required; `code` must equal the
  file name; `status` is the HTTP status clients branch on.
- Every code the backend can emit (canonical `arda-errors` constants, call
  sites of problem writers, auth-gateway machine slugs) must have a page —
  CI fails otherwise.
- Extra catalog pages are allowed (e.g. codes emitted dynamically by
  auth-gateway's message passthrough such as `tenant_context_unavailable`);
  the check reports them as informational orphans.
- Problem pages are operational contracts, not API implementation notes.
  Error messages may be localized or improved without changing the `code`
  or `type`.
- Treat deletion or renaming of a problem URL as a versioned API change.

## Known surfaces outside the gate

- Legacy `WriteAppError` endpoints emit the `{error: {code, message}}`
  envelope without a `type` URL; they are not covered by the catalog check
  and remain a migration debt.
- ai-service streaming payloads carry an `error.code` field (agent
  envelope); those are not `problem+json` and are catalogued only when they
  coincide with an emitted problem code.
