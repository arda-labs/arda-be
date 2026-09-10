// Cloudflare Worker serving the generated problem-catalog site
// (cloudflare/docs/dist). Assets-first: the worker only adds the JSON API
// endpoints used by clients (catalog lookup, nearest-code suggestions).
// Deploy: node scripts/build-problem-docs.mjs && bun x wrangler deploy -c cloudflare/docs/wrangler.jsonc

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    if (url.pathname === "/api/lookup" ) {
      return handleLookup(url, env);
    }
    return env.ASSETS.fetch(request);
  },
};

// GET /api/lookup?code=<problem code>
// → 200 { found: true, page: {...} } | 404 { found: false, suggestions: [...] }
async function handleLookup(url, env) {
  const code = (url.searchParams.get("code") ?? "").trim();
  if (!code) return json({ found: false, error: "missing code query parameter" }, 400);

  let index;
  try {
    const res = await env.ASSETS.fetch(new Request(new URL("/index.json", url)));
    if (!res.ok) return json({ found: false, error: "catalog index unavailable" }, 502);
    index = await res.json();
  } catch {
    return json({ found: false, error: "catalog index unavailable" }, 502);
  }

  const page = index.find((entry) => entry.code === code);
  if (page) {
    return json({
      found: true,
      page: { ...page, url: `https://docs.arda.io.vn/problems/${page.code}/` },
    });
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
    headers: { "content-type": "application/json", "access-control-allow-origin": "*" },
  });
}
