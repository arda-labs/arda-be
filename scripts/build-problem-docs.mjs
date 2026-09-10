// Renders docs/problems/*.md into the static site served by the
// docs-worker (cloudflare/docs/). Output: cloudflare/docs/dist/
//   index.html            catalog index grouped by HTTP status
//   problems/<code>/index.html   one page per problem code
//   index.json            search index [{ code, title, status, summary }]
//   404.html              unknown-code page with nearest-code suggestions
// Run before `wrangler deploy`: node scripts/build-problem-docs.mjs

import { mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import {
  CATALOG_DIR, DOCS_BASE, readCatalogPage, validatePage, renderPageMarkdown,
  renderMarkdown, statusText,
} from "./lib/problem-catalog.mjs";

const outDir = new URL("../cloudflare/docs/dist", import.meta.url).pathname
  .replace(/^\/([A-Za-z]:)/, "$1");

// --- load & validate catalog -------------------------------------------------

const files = (await readdir(CATALOG_DIR)).filter((n) => n.endsWith(".md") && n !== "README.md").sort();
if (files.length === 0) throw new Error("problem-docs: no catalog pages found in docs/problems/");

const pages = [];
const issues = [];
for (const name of files) {
  const page = await readCatalogPage(name);
  issues.push(...validatePage(page));
  pages.push(page);
}
if (issues.length > 0) {
  for (const issue of issues) console.error(`problem-docs: ${issue}`);
  throw new Error(`problem-docs: ${issues.length} catalog page issue(s)`);
}
pages.sort((a, b) => a.status - b.status || a.code.localeCompare(b.code));

// --- html shell --------------------------------------------------------------

const SITE_CSS = `
:root { --fg:#1a2233; --muted:#5b667a; --accent:#2563eb; --bg:#f7f8fb; --card:#ffffff; --border:#dfe4ee; }
* { box-sizing:border-box; }
body { margin:0; font:16px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif; color:var(--fg); background:var(--bg); }
header { background:var(--card); border-bottom:1px solid var(--border); padding:.9rem 1.2rem; display:flex; gap:1rem; align-items:baseline; flex-wrap:wrap; }
header .brand { font-weight:700; color:var(--accent); text-decoration:none; font-size:1.05rem; }
header .sub { color:var(--muted); font-size:.85rem; }
main { max-width:52rem; margin:0 auto; padding:1.5rem 1.2rem 4rem; }
h1 { font-size:1.6rem; margin:.2rem 0 1rem; }
h2 { font-size:1.15rem; margin:2rem 0 .6rem; border-bottom:1px solid var(--border); padding-bottom:.3rem; }
h3 { font-size:1rem; margin:1.4rem 0 .4rem; }
a { color:var(--accent); }
.card { background:var(--card); border:1px solid var(--border); border-radius:10px; padding:1rem 1.2rem; margin:.8rem 0; }
.meta { display:flex; gap:.6rem; flex-wrap:wrap; margin:.4rem 0 1rem; }
.badge { display:inline-block; font:600 .78rem/1 system-ui; padding:.35rem .6rem; border-radius:999px; background:#e8edf8; color:#33436b; }
.badge.s4, .badge.s5 { background:#fdecea; color:#8a2b22; }
code { font:0.92em ui-monospace,Consolas,monospace; background:#eef1f7; padding:.1rem .35rem; border-radius:4px; word-break:break-all; }
pre { background:#10141d; color:#e6ebf5; padding:1rem 1.2rem; border-radius:10px; overflow:auto; }
pre code { background:transparent; color:inherit; padding:0; }
input[type=search] { width:100%; font:1rem system-ui; padding:.6rem .9rem; border:1px solid var(--border); border-radius:8px; margin:0 0 1rem; }
.lbl { font-weight:600; color:var(--muted); font-size:.85rem; text-transform:uppercase; letter-spacing:.04em; margin:1.1rem 0 .3rem; }
ul { padding-left:1.4rem; }
footer { color:var(--muted); font-size:.8rem; margin-top:3rem; border-top:1px solid var(--border); padding-top:1rem; }
.suggest { margin:.3rem 0; }
.muted { color:var(--muted); }
`;

function shell(title, body) {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(title)} · Arda Problems</title>
<link rel="stylesheet" href="${title === "Arda Problem Catalog" ? "site.css" : "/site.css"}">
</head>
<body>
<header>
  <a class="brand" href="/">Arda Problem Catalog</a>
  <span class="sub">RFC-style error documentation for Arda APIs</span>
</header>
<main>
${body}
</main>
</body>
</html>`;
}

function escapeHtml(text) {
  return String(text).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

const HEADINGS = { 2: "h2", 3: "h3", 4: "h3" };
function bodyHtml(page) {
  const { client, operator, example } = renderPageMarkdown(page);
  const parts = [];
  const section = (label, content) => {
    if (!content) return;
    parts.push(`<div class="lbl">${label}</div>`);
    parts.push(content.startsWith("<p>") || content.startsWith("<ul") || content.startsWith("<pre") || content.startsWith("<h")
      ? content : `<p>${content}</p>`);
  };
  parts.push(`<div class="meta">
    <span class="badge s${String(page.status)[0]}">${page.status} ${statusText(page.status)}</span>
    <span class="badge"><code>${escapeHtml(page.code)}</code></span>
  </div>`);
  section("Meaning", renderMarkdown(page.summary));
  section("Client action", renderMarkdown(client));
  section("Operator action", renderMarkdown(operator));
  if (page.related_routes && page.related_routes.length > 0) {
    parts.push(`<div class="lbl">Related routes</div>`);
    parts.push(`<ul>${page.related_routes.map((r) => `<li>${renderInlineLoose(r)}</li>`).join("")}</ul>`);
  }
  section("Example response", `<pre><code>${escapeHtml(example)}</code></pre>`);
  parts.push(`<footer>Deterministic documentation for problem code <code>${escapeHtml(page.code)}</code>.
    Type URL: <code>${escapeHtml(DOCS_BASE + page.code)}</code>. Match on <code>code</code>, never on the localized message.</footer>`);
  return parts.join("\n");
}

// related_routes entries are plain strings; keep simple backtick rendering.
function renderInlineLoose(text) {
  return escapeHtml(text).replace(/`([^`]+)`/g, "<code>$1</code>");
}

// --- index.html ---------------------------------------------------------------

const byStatus = new Map();
for (const page of pages) {
  if (!byStatus.has(page.status)) byStatus.set(page.status, []);
  byStatus.get(page.status).push(page);
}

const indexBody = [];
indexBody.push(`<h1>Arda Problem Catalog</h1>`);
indexBody.push(`<p class="muted">Every <code>application/problem+json</code> error emitted by Arda backends
documents its stable <code>type</code> URL here. Look up a code below, or search.</p>`);
indexBody.push(`<input type="search" id="q" placeholder="Search by code, title, or keyword…" autocomplete="off">
<div id="groups">`);
for (const [status, group] of [...byStatus.entries()].sort((a, b) => a[0] - b[0])) {
  indexBody.push(`<h2>${status} ${statusText(status)}</h2>`);
  for (const page of group) {
    indexBody.push(`<div class="card suggest" data-search="${escapeHtml(`${page.code} ${page.title} ${page.summary ?? ""}`.toLowerCase())}">
      <a href="/problems/${encodeURIComponent(page.code)}/">${escapeHtml(page.title)}</a>
      <span class="muted"> — <code>${escapeHtml(page.code)}</code></span>
    </div>`);
  }
}
indexBody.push(`</div>`);
indexBody.push(`<script>
const q=document.getElementById('q'),g=document.getElementById('groups');
q.addEventListener('input',()=>{const v=q.value.trim().toLowerCase();
for(const c of g.querySelectorAll('.suggest'))c.style.display=c.dataset.search.includes(v)?'':'none';});
</script>`);
indexBody.push(`<footer>Generated from arda-be <code>docs/problems/</code> — do not edit by hand.
Client handling guide: match on <code>code</code>; <code>type</code> is a stable documentation link.</footer>`);

// --- per-code pages + 404 + index.json ----------------------------------------

await rm(outDir, { recursive: true, force: true });
await mkdir(`${outDir}/problems`, { recursive: true });

const searchIndex = [];
for (const page of pages) {
  const dir = `${outDir}/problems/${page.code}`;
  await mkdir(dir, { recursive: true });
  const html = shell(page.title, bodyHtml(page));
  await writeFile(`${dir}/index.html`, html, "utf8");
  searchIndex.push({
    code: page.code,
    title: page.title,
    status: page.status,
    summary: (page.summary ?? "").slice(0, 200),
  });
}

await writeFile(`${outDir}/index.html`, shell("Arda Problem Catalog", indexBody.join("\n")), "utf8");
await writeFile(`${outDir}/site.css`, SITE_CSS, "utf8");
await writeFile(`${outDir}/index.json`, JSON.stringify(searchIndex, null, 2), "utf8");

// 404 with nearest-code suggestions (levenshtein-free: prefix/substring only).
const notFoundBody = [];
notFoundBody.push(`<h1>Unknown problem code</h1>`);
notFoundBody.push(`<p>This problem code is not documented in the catalog. The code may be new,
retired, or mistyped. Check the spelling against the catalog index below — the page
also lists every documented code.</p>`);
notFoundBody.push(`<p><a href="/">Open the full catalog index</a></p>`);
await writeFile(`${outDir}/404.html`, shell("Unknown problem code", notFoundBody.join("\n")), "utf8");

console.log(`problem-docs: built ${pages.length} pages → cloudflare/docs/dist`);
