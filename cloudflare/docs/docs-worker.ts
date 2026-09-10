// Cloudflare Worker serving the generated problem-catalog site
// (cloudflare/docs/dist). Assets-first: the worker only adds the JSON API
// endpoints used by clients (catalog lookup, nearest-code suggestions).
// Deploy: node scripts/build-problem-docs.mjs && bun x wrangler deploy -c cloudflare/docs/wrangler.jsonc

interface Env {
  ASSETS: { fetch(request: Request): Promise<Response> };
}

interface CatalogIndexEntry {
  code: string;
  title: string;
  status: number;
  summary: string;
}

interface LookupPage extends CatalogIndexEntry {
  client_action?: string;
  operator_action?: string;
  related_routes?: string[];
  body?: string;
  url: string;
}

export default {
  async fetch(request, env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/api/lookup") {
      return handleLookup(url, env);
    }
    return env.ASSETS.fetch(request);
  },
} satisfies ExportedHandler<Env>;

// GET /api/lookup?code=<problem code>
// → 200 { found: true, page: {...} } | 404 { found: false, suggestions: [...] }
// The full response carries client_action/operator_action/related_routes (the
// remediation contract) so programmatic consumers — the AI agent's
// arda.docs.problemLookup tool, runbooks, IDE plugins — can explain and fix
// an error without scraping HTML. Rendered fields are plain text/markdown,
// not the raw front-matter.
async function handleLookup(url: URL, env: Env): Promise<Response> {
  const code = (url.searchParams.get("code") ?? "").trim();
  if (!code) return json({ found: false, error: "missing code query parameter" }, 400);

  const full = url.searchParams.get("full") !== "0";

  let index: CatalogIndexEntry[];
  try {
    const res = await env.ASSETS.fetch(new Request(new URL("/index.json", url)));
    if (!res.ok) return json({ found: false, error: "catalog index unavailable" }, 502);
    index = await res.json();
  } catch {
    return json({ found: false, error: "catalog index unavailable" }, 502);
  }

  const page = index.find((entry) => entry.code === code);
  if (page) {
    const base: LookupPage = {
      code: page.code,
      title: page.title,
      status: page.status,
      summary: page.summary,
      url: `https://docs.arda.io.vn/problems/${page.code}/`,
    };
    if (!full) return json({ found: true, page: base });

    // Merge the full page (client_action, operator_action, related_routes,
    // body) from the per-code document source. The build also emits
    // /problems/<code>/page.json for this purpose.
    try {
      const docRes = await env.ASSETS.fetch(new Request(new URL(`/problems/${page.code}/page.json`, url)));
      if (docRes.ok) {
        const doc = (await docRes.json()) as Partial<LookupPage>;
        return json({ found: true, page: { ...base, ...doc } });
      }
    } catch {
      // fall through to metadata-only response
    }
    return json({ found: true, page: base });
  }

  const lower = code.toLowerCase();
  const suggestions = index
    .filter((entry) => entry.code.toLowerCase().includes(lower) || lower.includes(entry.code.split(".")[0]))
    .slice(0, 8)
    .map((entry) => ({ code: entry.code, title: entry.title, status: entry.status }));
  return json({ found: false, suggestions }, 404);
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "content-type": "application/json",
      "access-control-allow-origin": "*",
      // The catalog updates on every docs deploy — never let the edge serve a
      // stale lookup (a cached pre-update response hid the full fields once).
      "cache-control": "no-store",
    },
  });
}
