# Problem docs site — docs.arda.io.vn

Static documentation site for Arda API problem codes
(`application/problem+json` `type` URLs).

- Content source of truth: [`docs/problems/`](../../docs/problems/) (one
  front-matter Markdown page per code).
- `scripts/check-problem-catalog.mjs` — CI gate: every emitted code must have
  a page here.
- `scripts/build-problem-docs.mjs` — renders `docs/problems/*.md` into
  `dist/` (index, per-code pages, `index.json`, 404).
- `docs-worker.ts` — assets-first Worker; adds `GET /api/lookup?code=…`
  (returns page metadata or nearest-code suggestions) for programmatic
  clients.

## Deploy (manual, like arda-mfe workers)

```powershell
node scripts/build-problem-docs.mjs
bun x wrangler deploy -c cloudflare/docs/wrangler.jsonc
```

The custom domain `docs.arda.io.vn` is provisioned by wrangler
(`custom_domain: true`) — the DNS record is created automatically on the
first deploy. `workers_dev` also exposes the worker on
`arda-problem-docs.<account>.workers.dev` as a fallback.
