// CI gate: every problem code that arda-be can emit must have a catalog page
// in docs/problems/. Closes the TODO in docs/problems/README.md ("Add a CI
// check that extracts every type URL from backend code ... verifies that a
// matching catalog page exists").
//
// Extraction strategy (no codegen, keep it simple and conservative):
//   1. Canonical constants from libs/go/arda-errors/errors.go (Code* = "...").
//   2. Two-pass call-site sweep over apps/ + libs/: pass 1 finds per-package
//      writer helpers (writeError/problem/respondRequestError/... that end up
//      in ardahttp.WriteProblem/WriteErrorCode), pass 2 extracts the code
//      argument at those call sites plus direct ardaerrors.New/Wrap sites.
//   3. auth-gateway snake slugs passed to respondRequestError/logProxyDenied.
// Strings that are clearly not problem codes (event types, permission ids,
// step codes) are filtered by emitter context, not by name alone.
//
// Flags:
//   --list              print the extracted codes and exit
//   --write-skeletons   generate docs/problems/<code>.md for missing codes
//
// Exit non-zero when any extracted code lacks a catalog page.

import { readdir, readFile, writeFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import {
  CATALOG_DIR, DOTTED_CODE_RE, SNAKE_SLUG_RE, readCatalogPage, validatePage,
  skeletonPage, deriveTitle, statusText,
} from "./lib/problem-catalog.mjs";

const root = new URL("..", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const argv = new Set(process.argv.slice(2));

// Best-effort status inference from the emitter call for skeleton generation
// and error hints; wrong guesses only affect the hint text, never the gate.
const STATUS_NAMES = {
  BadRequest: 400, Unauthorized: 401, Forbidden: 403, NotFound: 404,
  MethodNotAllowed: 405, Conflict: 409, TooManyRequests: 429,
  InternalServerError: 500, BadGateway: 502, ServiceUnavailable: 503,
  GatewayTimeout: 504,
};

function guessStatus(contextLine) {
  const m = contextLine.match(/http\.Status(\w+)/);
  if (m && STATUS_NAMES[m[1]]) return STATUS_NAMES[m[1]];
  const numeric = contextLine.match(/\b(4\d\d|5\d\d)\b/);
  return numeric ? Number(numeric[1]) : 500;
}

// --- collect Go sources (skip tests, generated proto code) ------------------

async function walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true }).catch(() => []);
  const files = [];
  for (const entry of entries) {
    const path = `${dir}/${entry.name}`;
    if (entry.isDirectory()) {
      if (entry.name === "testdata" || entry.name === "mocks") continue;
      files.push(...(await walk(path)));
    } else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go") && !entry.name.endsWith(".pb.go")) {
      files.push(path);
    }
  }
  return files;
}

const goFiles = [
  ...(await walk(`${root}/apps`)),
  ...(await walk(`${root}/libs`)),
  ...(await walk(`${root}/tools`)),
];

// --- 1. canonical constants from arda-errors -------------------------------

const canonical = new Map(); // code → { origin }
{
  const src = await readFile(`${root}/libs/go/arda-errors/errors.go`, "utf8");
  for (const m of src.matchAll(/Code\w+\s*=\s*"([a-z][a-z0-9_.-]+)"/g)) {
    canonical.set(m[1], "arda-errors");
  }
  if (canonical.size < 10) throw new Error("problem-catalog: failed to parse arda-errors constants");
}

// --- 2. call-site sweep ------------------------------------------------------

const relayFns = new Set(["WriteProblem", "WriteErrorCode"]);
const slugFns = new Set(["respondRequestError", "logProxyDenied"]);
const emitCodes = new Map(); // code → [{ file, line, context }]

function record(code, file, line, context) {
  if (!DOTTED_CODE_RE.test(code) && !SNAKE_SLUG_RE.test(code)) return;
  if (!emitCodes.has(code)) emitCodes.set(code, []);
  const sites = emitCodes.get(code);
  if (sites.length < 3) sites.push({ file, line, context });
}

// Pass 1 (package-scoped): Go helpers like writeError/problem live in
// http_response.go while their call sites are spread across the package, so
// emitter detection must key on the package directory, not the file.
const helpersByDir = new Map(); // dir → Set(helper fn names)
const sources = new Map(); // file → { src, lines, rel, dir }
for (const file of goFiles) {
  const src = await readFile(file, "utf8");
  const rel = file.slice(root.length + 1);
  const dir = rel.replace(/\/[^/]+$/, "");
  sources.set(file, { src, lines: src.split("\n"), rel, dir });
  const helpers = helpersByDir.get(dir) ?? new Set();
  const lines = src.split("\n");
  const bodyByFn = new Map();
  for (let i = 0; i < lines.length; i++) {
    const m = lines[i].match(/^func (\w+)\(/);
    if (m) {
      const body = [];
      for (let j = i; j < Math.min(i + 15, lines.length); j++) body.push(lines[j]);
      bodyByFn.set(m[1], body.join("\n"));
    }
  }
  for (const [fn, body] of bodyByFn) {
    const relays = [...body.matchAll(/(?:ardahttp\.)?(?:WriteProblem|WriteErrorCode)\(/g)].length > 0
      || /problems\/%s|ProblemsTypeBaseURL/.test(body); // hand-rolled problem JSON writers
    const takesCodeArg = /^func \w+\([^)]*\bcode\b[^)]*\)/m.test(body.split("\n")[0] ?? "");
    if (relays && takesCodeArg && !relayFns.has(fn)) helpers.add(fn);
  }
  helpersByDir.set(dir, helpers);
}

// Pass 2: extract the code literal at emitter call sites.
for (const { lines, rel, dir } of sources.values()) {
  const helpers = helpersByDir.get(dir) ?? new Set();
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    // Direct canonical emitters.
    const direct = line.match(/(?:ardaerrors\.)?New\(\s*"([a-z][a-z0-9_.-]+)"/);
    if (direct && (line.includes("WriteProblem") || line.includes("WriteErrorCode"))) {
      record(direct[1], rel, i + 1, line.trim());
    }
    // Relay emitters (ardahttp.WriteProblem/WriteErrorCode + local helpers).
    for (const fn of [...relayFns, ...helpers]) {
      const re = new RegExp(`\\b${fn}\\([^)]*?"([a-z][a-z0-9_.-]+)"`);
      const m = line.match(re);
      if (m) record(m[1], rel, i + 1, line.trim());
    }
    // auth-gateway machine slugs.
    for (const fn of slugFns) {
      const re = new RegExp(`\\b${fn}\\([^)]*?"([a-z0-9_.-]+)"`);
      const m = line.match(re);
      if (m) record(m[1], rel, i + 1, line.trim());
    }
  }
}

// --- 3. catalog pages ---------------------------------------------------------

const pages = new Map(); // code → filename
const issues = [];
{
  const files = (await readdir(CATALOG_DIR)).filter((n) => n.endsWith(".md") && n !== "README.md");
  for (const name of files) {
    const page = await readCatalogPage(name);
    issues.push(...validatePage(page));
    if (typeof page.code === "string" && page.code.length > 0) {
      if (pages.has(page.code)) issues.push(`${name}: duplicate catalog page for code ${page.code}`);
      pages.set(page.code, name);
    }
  }
}

// --- report -------------------------------------------------------------------

const extracted = [...emitCodes.keys()].sort();
const known = new Set([...canonical.keys(), ...extracted]);
const missing = [...known].filter((code) => !pages.has(code)).sort();
const orphaned = [...pages.keys()].filter((code) => !known.has(code)).sort();

if (argv.has("--list")) {
  const groups = new Map();
  for (const code of [...known].sort()) {
    const group = code.split(".")[0] === "validation" || code.split(".")[0] === "common" || code.split(".")[0] === "auth"
      || code.split(".")[0] === "tenant" || code.split(".")[0] === "iam"
      ? code.split(".")[0]
      : code.split(".")[0] === "workflow" ? "workflow" : code.split(".")[0];
    const bucket = canonical.has(code) && !emitCodes.has(code) ? `${group} (constant only)` : group;
    if (!groups.has(bucket)) groups.set(bucket, []);
    groups.get(bucket).push(code);
  }
  for (const [bucket, codes] of [...groups.entries()].sort()) {
    console.log(`\n# ${bucket} (${codes.length})`);
    for (const code of codes) console.log(`  ${code}`);
  }
  console.log(`\ncatalog pages: ${pages.size}, known codes: ${known.size}, missing: ${missing.length}, orphaned: ${orphaned.length}`);
  process.exit(0);
}

if (missing.length > 0 && argv.has("--write-skeletons")) {
  for (const code of missing) {
    const from = emitCodes.get(code)?.[0];
    const status = from ? guessStatus(from.context) : 500;
    await writeFile(new URL(`${code}.md`, CATALOG_DIR), skeletonPage(code, status), "utf8");
    console.log(`problem-catalog: wrote skeleton docs/problems/${code}.md (status ${status})`);
  }
  console.log("problem-catalog: re-run the check after reviewing the skeletons");
  process.exit(0);
}

for (const issue of issues) console.error(`problem-catalog: ${issue}`);

let hinted = 0;
for (const code of missing) {
  const site = emitCodes.get(code)?.[0];
  const status = site ? guessStatus(site.context) : null;
  console.error(
    `problem-catalog: missing docs/problems/${code}.md`
      + (site ? ` (emitted at ${site.file}:${site.line} → HTTP ${status ?? "?"})` : " (canonical constant)"),
  );
  hinted++;
}
for (const code of orphaned) {
  console.error(`problem-catalog: catalog page ${code} is never emitted by backend code (keep or remove deliberately)`);
}
if (issues.length > 0 || missing.length > 0) {
  throw new Error(`problem-catalog check failed: ${missing.length} missing, ${orphaned.length} orphaned, ${issues.length} page issue(s)`);
}

console.log(`problem-catalog OK: ${known.size} codes (${canonical.size} canonical, ${extracted.length} call-site) all have catalog pages (${pages.size} pages)`);

// keep deriveTitle/statusText imported for --write-skeletons consumers parity
void deriveTitle; void statusText;
