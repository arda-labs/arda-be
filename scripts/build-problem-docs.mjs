// Renders docs/problems/*.md into the static site served by the
// docs-worker (cloudflare/docs/). Output: cloudflare/docs/dist/
//   index.html                     catalog index (hero + search + grouped cards)
//   problems/<code>/index.html     one page per problem code
//   problems/<code>/page.json      machine-readable page for /api/lookup
//   index.json                     search index (metadata only)
//   404.html                       unknown-code page
//   site.css, app.js               design system + interactions
// Run before `wrangler deploy`: node scripts/build-problem-docs.mjs
// Dependency-free by design: hand-rolled templates, no framework, no CDN.

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

const generatedAt = new Date().toISOString().slice(0, 16).replace("T", " ") + " UTC";
const domainOf = (code) => code.split(".")[0];

// --- design system -----------------------------------------------------------

const SITE_CSS = `
:root {
  --bg:#f6f7fb; --bg-accent:#eef2fb; --card:#ffffff; --border:#e3e7f0;
  --fg:#16203a; --muted:#5d6b8a; --faint:#8b96ad;
  --accent:#2f5fe0; --accent-soft:#e8eeff; --accent-ink:#1d3fae;
  --ok:#0f8a4d; --warn:#b45309; --err:#be123c; --server:#7c3aed;
  --code-bg:#0f1526; --code-fg:#dbe4f5;
  --shadow:0 1px 2px rgba(22,32,58,.05),0 8px 24px -12px rgba(22,32,58,.12);
  --mono:ui-monospace,"Cascadia Code",Consolas,"SF Mono",Menlo,monospace;
}
[data-theme="dark"] {
  --bg:#0c1120; --bg-accent:#111a30; --card:#141c31; --border:#24304d;
  --fg:#e5ebf8; --muted:#9aa7c4; --faint:#6b7893;
  --accent:#7ea4ff; --accent-soft:#1a2647; --accent-ink:#a9c2ff;
  --ok:#4ade80; --warn:#fbbf24; --err:#fb7185; --server:#c4b5fd;
  --code-bg:#0a0f1e; --code-fg:#d5dff2;
  --shadow:0 1px 2px rgba(0,0,0,.4),0 10px 30px -12px rgba(0,0,0,.5);
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
    --bg:#0c1120; --bg-accent:#111a30; --card:#141c31; --border:#24304d;
    --fg:#e5ebf8; --muted:#9aa7c4; --faint:#6b7893;
    --accent:#7ea4ff; --accent-soft:#1a2647; --accent-ink:#a9c2ff;
    --ok:#4ade80; --warn:#fbbf24; --err:#fb7185; --server:#c4b5fd;
    --code-bg:#0a0f1e; --code-fg:#d5dff2;
    --shadow:0 1px 2px rgba(0,0,0,.4),0 10px 30px -12px rgba(0,0,0,.5);
  }
}
* { box-sizing:border-box; }
html { scroll-behavior:smooth; }
body { margin:0; font:16px/1.65 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;
  color:var(--fg); background:var(--bg); -webkit-font-smoothing:antialiased; }
a { color:var(--accent); text-decoration:none; }
a:hover { text-decoration:underline; text-underline-offset:3px; }
code { font-family:var(--mono); font-size:.86em; background:var(--accent-soft);
  color:var(--accent-ink); padding:.12em .4em; border-radius:6px; word-break:break-all; }

/* header */
.topbar { position:sticky; top:0; z-index:10; backdrop-filter:blur(10px);
  background:color-mix(in srgb, var(--bg) 82%, transparent);
  border-bottom:1px solid var(--border); }
.topbar-inner { max-width:64rem; margin:0 auto; padding:.7rem 1.2rem;
  display:flex; align-items:center; gap:.9rem; }
.brand { display:flex; align-items:center; gap:.55rem; font-weight:700;
  color:var(--fg); font-size:1rem; letter-spacing:-.01em; }
.brand:hover { text-decoration:none; }
.brand .mark { width:26px; height:26px; border-radius:8px; flex:none;
  background:linear-gradient(135deg,var(--accent),#8b5cf6);
  display:grid; place-items:center; color:#fff; font:700 14px var(--mono); }
.brand .sub { color:var(--faint); font-weight:500; font-size:.82rem; margin-left:.25rem; }
.topbar .spacer { flex:1; }
.theme-btn { border:1px solid var(--border); background:var(--card); color:var(--muted);
  width:34px; height:34px; border-radius:9px; cursor:pointer; display:grid; place-items:center; }
.theme-btn:hover { color:var(--accent); border-color:var(--accent); }
.theme-btn svg { width:17px; height:17px; }

main { max-width:64rem; margin:0 auto; padding:1.8rem 1.2rem 4rem; }

/* index hero */
.hero { padding:1.2rem 0 .4rem; }
.hero h1 { font-size:2rem; letter-spacing:-.02em; margin:0 0 .5rem; }
.hero p.lede { color:var(--muted); max-width:44rem; margin:0 0 1.1rem; font-size:1.02rem; }
.stats { display:flex; gap:.6rem; flex-wrap:wrap; }
.stat { background:var(--card); border:1px solid var(--border); border-radius:10px;
  padding:.45rem .85rem; font-size:.85rem; color:var(--muted); box-shadow:var(--shadow); }
.stat b { color:var(--fg); font-size:1rem; margin-right:.3rem; font-family:var(--mono); }

/* search */
.search-wrap { position:relative; margin:1.4rem 0 .4rem; }
.search-wrap svg { position:absolute; left:.9rem; top:50%; transform:translateY(-50%);
  width:17px; height:17px; color:var(--faint); pointer-events:none; }
#q { width:100%; font:1rem system-ui; color:var(--fg); padding:.72rem 6.2rem .72rem 2.6rem;
  border:1px solid var(--border); border-radius:12px; background:var(--card);
  box-shadow:var(--shadow); outline:none; transition:border-color .15s; }
#q:focus { border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-soft); }
.kbd { position:absolute; right:.8rem; top:50%; transform:translateY(-50%);
  font:600 .72rem var(--mono); color:var(--faint); border:1px solid var(--border);
  border-radius:6px; padding:.15rem .45rem; background:var(--bg-accent); }
.result-count { color:var(--faint); font-size:.85rem; margin:.4rem 0 0; }

/* status groups */
.group { margin-top:2rem; }
.group > h2 { display:flex; align-items:center; gap:.6rem; font-size:1.08rem;
  letter-spacing:-.01em; margin:0 0 .9rem; }
.group > h2 .dot { width:10px; height:10px; border-radius:50%; flex:none; }
.dot.c4 { background:var(--warn); } .dot.c5 { background:var(--err); }
.group > h2 .cnt { color:var(--faint); font-weight:500; font-size:.85rem; }
.grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(235px,1fr)); gap:.7rem; }
.pcard { background:var(--card); border:1px solid var(--border); border-radius:12px;
  padding:.75rem .9rem; display:flex; flex-direction:column; gap:.2rem;
  box-shadow:var(--shadow); transition:transform .12s, border-color .12s; }
.pcard:hover { transform:translateY(-2px); border-color:var(--accent); text-decoration:none; }
.pcard .title { font-weight:600; color:var(--fg); font-size:.94rem; line-height:1.35; }
.pcard:hover .title { color:var(--accent); }
.pcard .code { font-family:var(--mono); font-size:.76rem; color:var(--faint); word-break:break-all; }
.pcard .badge-line { display:flex; gap:.4rem; align-items:center; }
.mini-badge { font:600 .68rem/1 var(--mono); padding:.28rem .5rem; border-radius:6px;
  background:var(--bg-accent); color:var(--muted); }
.mini-badge.s4 { color:var(--warn); } .mini-badge.s5 { color:var(--err); }
.empty-note { color:var(--faint); padding:1rem 0; display:none; }

/* problem page */
.crumbs { font-size:.85rem; color:var(--faint); margin-bottom:1rem; }
.crumbs a { color:var(--muted); }
h1.ptitle { font-size:1.75rem; letter-spacing:-.02em; margin:0 0 .7rem; }
.meta { display:flex; gap:.5rem; flex-wrap:wrap; margin:0 0 1.4rem; align-items:center; }
.badge { display:inline-flex; align-items:center; gap:.4rem; font:600 .8rem/1 var(--mono);
  padding:.42rem .7rem; border-radius:8px; border:1px solid var(--border); background:var(--card); }
.badge.s4 { color:var(--warn); } .badge.s5 { color:var(--err); }
.section-label { font-weight:700; color:var(--faint); font-size:.74rem;
  text-transform:uppercase; letter-spacing:.09em; margin:1.6rem 0 .45rem;
  display:flex; align-items:center; gap:.5rem; }
.section-label::after { content:""; flex:1; height:1px; background:var(--border); }
.prose p { margin:.4rem 0; } .prose ul { margin:.4rem 0; padding-left:1.35rem; }
.prose li { margin:.22rem 0; }
.chips { display:flex; gap:.45rem; flex-wrap:wrap; }
.chip { font:500 .8rem var(--mono); color:var(--muted); background:var(--card);
  border:1px solid var(--border); border-radius:999px; padding:.32rem .7rem; }

/* example block */
.codeblock { border-radius:12px; overflow:hidden; border:1px solid var(--border); box-shadow:var(--shadow); }
.codeblock .bar { display:flex; align-items:center; gap:.55rem; padding:.5rem .9rem;
  background:var(--bg-accent); border-bottom:1px solid var(--border); }
.codeblock .bar .lights { display:flex; gap:5px; }
.codeblock .bar .lights i { width:10px; height:10px; border-radius:50%; background:var(--border); }
.codeblock .bar .lang { font:600 .72rem var(--mono); color:var(--faint); letter-spacing:.06em; }
.codeblock .bar .spacer { flex:1; }
.copy-btn { display:inline-flex; align-items:center; gap:.35rem; font:600 .74rem system-ui;
  color:var(--muted); background:transparent; border:1px solid var(--border);
  border-radius:7px; padding:.28rem .6rem; cursor:pointer; }
.copy-btn:hover { color:var(--accent); border-color:var(--accent); }
.copy-btn svg { width:13px; height:13px; }
.codeblock pre { margin:0; background:var(--code-bg); color:var(--code-fg);
  padding:1.1rem 1.25rem; overflow:auto; font:13px/1.6 var(--mono); }
.codeblock pre code { background:none; color:inherit; padding:0; font-size:inherit; }
.tok-key { color:#8ab4ff; } .tok-str { color:#9ece6a; }
.tok-num { color:#ff9e64; } .tok-bool { color:#bb9af7; }

/* similar problems */
.similar { margin-top:2rem; border-top:1px dashed var(--border); padding-top:1rem; }
.similar .links { display:flex; flex-direction:column; gap:.3rem; }
.similar a { display:flex; justify-content:space-between; gap:1rem; padding:.35rem .2rem;
  border-radius:8px; font-size:.92rem; }
.similar a:hover { background:var(--bg-accent); text-decoration:none; }
.similar a .st { color:var(--faint); font:600 .78rem var(--mono); }

footer.site { max-width:64rem; margin:0 auto; padding:1.2rem 1.2rem 3rem; color:var(--faint);
  font-size:.8rem; border-top:1px solid var(--border); }
footer.site code { font-size:.78rem; }
`;

const ICONS = {
  search: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>',
  copy: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>',
  sun: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>',
  moon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z"/></svg>',
};

const APP_JS = `
(function(){
  var root=document.documentElement;
  var saved=null; try{saved=localStorage.getItem('arda-docs-theme')}catch(e){}
  if(saved==='dark'||saved==='light') root.setAttribute('data-theme',saved);
  var btn=document.getElementById('themeToggle');
  if(btn){
    var paint=function(){btn.innerHTML=root.getAttribute('data-theme')==='dark'?'${ICONS.sun}':'${ICONS.moon}'};
    paint();
    btn.addEventListener('click',function(){
      var next=root.getAttribute('data-theme')==='dark'?'light':'dark';
      root.setAttribute('data-theme',next);
      try{localStorage.setItem('arda-docs-theme',next)}catch(e){}
      paint();
    });
  }
  var q=document.getElementById('q');
  if(q){
    var count=document.getElementById('resultCount');
    var cards=Array.prototype.slice.call(document.querySelectorAll('.pcard'));
    var groups=Array.prototype.slice.call(document.querySelectorAll('.group'));
    var note=document.getElementById('emptyNote');
    var apply=function(){
      var v=q.value.trim().toLowerCase(),shown=0;
      cards.forEach(function(c){
        var hit=!v||c.dataset.search.indexOf(v)>=0;
        c.style.display=hit?'':'none'; if(hit)shown++;
      });
      groups.forEach(function(g){
        var any=g.querySelectorAll('.pcard:not([style*="none"])').length>0;
        g.style.display=any?'':'none';
      });
      if(count)count.textContent=shown+' of '+cards.length+' problems';
      if(note)note.style.display=shown===0?'block':'none';
    };
    q.addEventListener('input',apply);
    document.addEventListener('keydown',function(e){
      if(e.key==='/'&&document.activeElement!==q&&q.offsetParent!==null){e.preventDefault();q.focus();}
    });
  }
  document.querySelectorAll('.copy-btn').forEach(function(b){
    b.addEventListener('click',function(){
      var src=b.closest('.codeblock').querySelector('pre code');
      navigator.clipboard.writeText(src.textContent).then(function(){
        var old=b.dataset.label||b.textContent; b.dataset.label=old;
        b.textContent='Copied'; setTimeout(function(){b.textContent=old;},1400);
      }).catch(function(){});
    });
  });
})();
`;

function escapeHtml(text) {
  return String(text).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

const FAVICON = "data:image/svg+xml," + encodeURIComponent(
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="8" fill="#2f5fe0"/><text x="16" y="22" font-family="monospace" font-size="17" font-weight="bold" fill="#fff" text-anchor="middle">A</text></svg>',
);

function shell(title, description, body) {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="${escapeHtml(description)}">
<title>${escapeHtml(title)} · Arda Problems</title>
<link rel="icon" href="${FAVICON}">
<link rel="stylesheet" href="${title === "Arda Problem Catalog" ? "site.css" : "/site.css"}">
<script>
(function(){var s=null;try{s=localStorage.getItem('arda-docs-theme')}catch(e){}
if(s==='dark')document.documentElement.setAttribute('data-theme','dark');})();
</script>
</head>
<body>
<header class="topbar"><div class="topbar-inner">
  <a class="brand" href="/"><span class="mark">A</span>Arda Problems<span class="sub">problem catalog</span></a>
  <span class="spacer"></span>
  <button class="theme-btn" id="themeToggle" aria-label="Toggle color theme"></button>
</div></header>
<main>
${body}
</main>
<footer class="site">Deterministic documentation generated from arda-be <code>docs/problems/</code> · built ${generatedAt} · client contract: match on <code>code</code>, <code>type</code> is a stable documentation link · programmatic access: <code>GET /api/lookup?code=&lt;code&gt;</code></footer>
<script src="${title === "Arda Problem Catalog" ? "app.js" : "/app.js"}"></script>
</body>
</html>`;
}

const HEADINGS = { 2: "h2", 3: "h3", 4: "h3" };
function bodyHtml(page, similar) {
  const { client, operator, example } = renderPageMarkdown(page);
  const parts = [];
  const cls = String(page.status)[0] === "4" ? "s4" : "s5";
  parts.push(`<nav class="crumbs"><a href="/">Catalog</a> / ${page.status} ${statusText(page.status)}</nav>`);
  parts.push(`<h1 class="ptitle">${escapeHtml(page.title)}</h1>`);
  parts.push(`<div class="meta">
    <span class="badge ${cls}">${page.status} ${statusText(page.status)}</span>
    <span class="badge"><code>${escapeHtml(page.code)}</code></span>
    ${domainOf(page.code) !== page.code ? `<span class="badge">domain: ${escapeHtml(domainOf(page.code))}</span>` : ""}
  </div>`);
  const section = (label, content) => {
    if (!content) return;
    parts.push(`<div class="section-label">${label}</div>`);
    parts.push(content.startsWith("<p>") || content.startsWith("<ul") || content.startsWith("<pre") || content.startsWith("<h")
      ? `<div class="prose">${content}</div>` : `<div class="prose"><p>${content}</p></div>`);
  };
  section("Meaning", renderMarkdown(page.summary));
  section("What the client should do", renderMarkdown(client));
  section("What operators should check", renderMarkdown(operator));
  if (page.related_routes && page.related_routes.length > 0) {
    parts.push(`<div class="section-label">Related routes</div>`);
    parts.push(`<div class="chips">${page.related_routes.map((r) => `<span class="chip">${renderInlineLoose(r)}</span>`).join("")}</div>`);
  }
  section("Example response", `<div class="codeblock"><div class="bar"><span class="lights"><i></i><i></i><i></i></span><span class="lang">HTTP · problem+json</span><span class="spacer"></span><button class="copy-btn" type="button">${ICONS.copy} Copy</button></div><pre><code>${highlightJSON(example)}</code></pre></div>`);

  if (similar.length > 0) {
    parts.push(`<div class="similar"><div class="section-label">Related problems</div><div class="links">`);
    for (const s of similar) {
      parts.push(`<a href="/problems/${encodeURIComponent(s.code)}/"><span>${escapeHtml(s.title)}</span><span class="st">${s.status} · ${escapeHtml(s.code)}</span></a>`);
    }
    parts.push(`</div></div>`);
  }
  return parts.join("\n");
}

// related_routes entries are plain strings; keep simple backtick rendering.
function renderInlineLoose(text) {
  return escapeHtml(text).replace(/`([^`]+)`/g, "<code>$1</code>");
}

// JSON-aware token highlighter for the example block (build-time, no client JS).
function highlightJSON(text) {
  const re = /("(?:[^"\\]|\\.)*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;
  let out = "";
  let last = 0;
  for (const m of text.matchAll(re)) {
    out += escapeHtml(text.slice(last, m.index));
    if (m[1] !== undefined) {
      out += m[2]
        ? `<span class="tok-key">${escapeHtml(m[1])}</span>${escapeHtml(m[2])}`
        : `<span class="tok-str">${escapeHtml(m[1])}</span>`;
    } else if (m[3] !== undefined) {
      out += `<span class="tok-bool">${m[3]}</span>`;
    } else {
      out += `<span class="tok-num">${m[4]}</span>`;
    }
    last = m.index + m[0].length;
  }
  out += escapeHtml(text.slice(last));
  return out;
}

// --- index.html ---------------------------------------------------------------

const byStatus = new Map();
for (const page of pages) {
  if (!byStatus.has(page.status)) byStatus.set(page.status, []);
  byStatus.get(page.status).push(page);
}
const clientCount = pages.filter((p) => p.status < 500).length;
const serverCount = pages.length - clientCount;

// --- grouped grid (shared by index and 404) -----------------------------------

function groupedGridHTML() {
  const out = [];
  for (const [status, group] of [...byStatus.entries()].sort((a, b) => a[0] - b[0])) {
    out.push(`<section class="group"><h2><span class="dot c${String(status)[0]}"></span>${status} ${statusText(status)} <span class="cnt">· ${group.length}</span></h2><div class="grid">`);
    for (const page of group) {
      out.push(`<a class="pcard" href="/problems/${encodeURIComponent(page.code)}/" data-search="${escapeHtml(`${page.code} ${page.title} ${page.summary ?? ""}`.toLowerCase())}">
        <span class="badge-line"><span class="mini-badge s${String(status)[0]}">${status}</span><span class="code">${escapeHtml(page.code)}</span></span>
        <span class="title">${escapeHtml(page.title)}</span>
      </a>`);
    }
    out.push(`</div></section>`);
  }
  return out.join("\n");
}

const indexBody = [];
indexBody.push(`<section class="hero">
  <h1>Arda Problem Catalog</h1>
  <p class="lede">Every <code>application/problem+json</code> error emitted by Arda backends carries a stable
  <code>type</code> URL documented here — what it means, what the client should do, and what operators should check.</p>
  <div class="stats">
    <span class="stat"><b>${pages.length}</b>documented problems</span>
    <span class="stat"><b>${clientCount}</b>client errors (4xx)</span>
    <span class="stat"><b>${serverCount}</b>server errors (5xx)</span>
    <span class="stat"><b>${new Set(pages.map((p) => domainOf(p.code))).size}</b>domains</span>
  </div>
</section>
<div class="search-wrap">
  ${ICONS.search}
  <input type="search" id="q" placeholder="Search by code, title, or keyword…" autocomplete="off" spellcheck="false">
  <span class="kbd">/</span>
</div>
<p class="result-count" id="resultCount">${pages.length} of ${pages.length} problems</p>
<div id="groups">
${groupedGridHTML()}
</div>
<p class="empty-note" id="emptyNote">No problem matches your search. Try a shorter keyword, or the <code>code</code> exactly as returned in the error response.</p>`);

// --- per-code pages + 404 + index.json ----------------------------------------

// Upsert instead of a wholesale rm: Windows keeps dist locked by indexers and
// stray workerd processes, so rmdir on the root fails spuriously. Stale
// per-code directories are pruned individually below.
await mkdir(`${outDir}/problems`, { recursive: true });

const searchIndex = [];
for (const page of pages) {
  const dir = `${outDir}/problems/${page.code}`;
  await mkdir(dir, { recursive: true });
  const similar = pages
    .filter((p) => p.code !== page.code && domainOf(p.code) === domainOf(page.code))
    .slice(0, 6);
  const html = shell(page.title, `${page.status} ${statusText(page.status)} — ${page.summary ?? page.title}`, bodyHtml(page, similar));
  await writeFile(`${dir}/index.html`, html, "utf8");
  const { client, operator, example } = renderPageMarkdown(page);
  await writeFile(
    `${dir}/page.json`,
    JSON.stringify(
      {
        code: page.code,
        title: page.title,
        status: page.status,
        summary: page.summary ?? "",
        client_action: client,
        operator_action: operator,
        related_routes: page.related_routes ?? [],
        body: page._body ?? "",
        example,
      },
      null,
      2,
    ),
    "utf8",
  );
  searchIndex.push({
    code: page.code,
    title: page.title,
    status: page.status,
    summary: (page.summary ?? "").slice(0, 200),
  });
}

await writeFile(`${outDir}/index.html`, shell("Arda Problem Catalog", "Stable documentation for every Arda API error code (RFC-style problem+json type URLs).", indexBody.join("\n")), "utf8");
await writeFile(`${outDir}/site.css`, SITE_CSS, "utf8");
await writeFile(`${outDir}/app.js`, APP_JS, "utf8");
await writeFile(`${outDir}/index.json`, JSON.stringify(searchIndex, null, 2), "utf8");

// 404 shows the unknown code inline plus the full searchable catalog grid.
const notFoundBody = [];
notFoundBody.push(`<section class="hero"><h1>Unknown problem code</h1>
<p class="lede" id="nfLede">This problem code is not documented in the catalog. It may be new, retired, or
mistyped — check the spelling against the full catalog below, or search for a keyword.</p></section>
<div class="search-wrap">
  ${ICONS.search}
  <input type="search" id="q" placeholder="Search the catalog…" autocomplete="off" spellcheck="false">
  <span class="kbd">/</span>
</div>
<p class="result-count" id="resultCount"></p>
<div id="groups">
${groupedGridHTML()}
</div>
<p class="empty-note" id="emptyNote">No match. <a href="/">Open the full catalog index</a>.</p>
<script>
(function(){
  var code=decodeURIComponent(location.pathname.split("/").filter(Boolean).pop()||"");
  var el=document.getElementById("nfLede");
  if(code&&el)el.innerHTML="The problem code <code>"+code.replace(/[<>&]/g,"")+"</code> is not documented in the catalog. It may be new, retired, or mistyped — check the spelling against the full catalog below, or search for a keyword.";
})();
</script>`);
await writeFile(`${outDir}/404.html`, shell("Unknown problem code", "Unknown problem code — not documented in the Arda problem catalog.", notFoundBody.join("\n")), "utf8");

console.log(`problem-docs: built ${pages.length} pages → cloudflare/docs/dist`);

// Prune per-code directories that no longer correspond to a catalog page.
const live = new Set(pages.map((p) => p.code));
for (const entry of await readdir(`${outDir}/problems`, { withFileTypes: true })) {
  if (entry.isDirectory() && !live.has(entry.name)) {
    await rm(`${outDir}/problems/${entry.name}`, { recursive: true, force: true }).catch(() => {});
    console.log(`problem-docs: pruned stale problems/${entry.name}/`);
  }
}
