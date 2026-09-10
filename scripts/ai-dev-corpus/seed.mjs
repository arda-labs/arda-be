#!/usr/bin/env node
// Dev-only seeder: pushes a synthetic markdown corpus through the real
// knowledge pipeline (create source -> version -> review -> publish).
// Signs requests with the shared workload secret, same as auth-gateway does.
import { createHmac, randomBytes } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { join, basename } from "node:path";

const BASE = process.env.BASE ?? "http://127.0.0.1:18080";
const SECRET = process.env.SECRET ?? "";
const TENANT = process.env.TENANT ?? "00000000-0000-0000-0000-000000000010";
const AUTHOR = "corpus-author";
const REVIEWER = "corpus-reviewer";

if (!SECRET || SECRET.length < 32) {
  console.error("SECRET env is required (ARDA_SERVICE_AUTH_SECRET)");
  process.exit(1);
}

function signToken() {
  const now = Math.floor(Date.now() / 1000);
  const claims = { v: "v1", src: "auth-gateway", aud: "ai-service", iat: now, exp: now + 120, nonce: randomBytes(16).toString("base64url") };
  const payload = Buffer.from(JSON.stringify(claims)).toString("base64url");
  const sig = createHmac("sha256", SECRET).update("v1." + payload).digest("base64url");
  return `v1.${payload}.${sig}`;
}

async function api(method, path, body, user = AUTHOR) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      "x-service-auth": signToken(),
      "X-Auth-Checked": "true",
      "X-User-Id": user,
      "X-Tenant-Id": TENANT,
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let json;
  try { json = JSON.parse(text); } catch { json = text; }
  if (!res.ok) throw new Error(`${method} ${path} -> ${res.status}: ${text.slice(0, 300)}`);
  return json;
}

const docsDir = join(import.meta.dirname, "docs");
const files = readdirSync(docsDir).filter((f) => f.endsWith(".md")).sort();

const existingTitles = new Set((await api("GET", "/api/rag/sources?limit=200")).map((s) => s.title));

const results = [];
for (const file of files) {
  const content = readFileSync(join(docsDir, file), "utf8");
  const title = content.split("\n")[0].replace(/^#\s*/, "");
  const slug = basename(file, ".md");
  if (existingTitles.has(title)) {
    results.push({ file, skipped: true });
    console.log(`SKIP ${file} (source exists)`);
    continue;
  }
  try {
    const src = await api("POST", "/api/rag/sources", {
      title,
      description: `Tài liệu giả lập dev: ${title}`,
      source_type: "markdown",
      scope: "tenant",
      classification: "internal",
      language: "vi",
      tags: ["dev", "synthetic", slug.split("-")[1] ?? "misc"],
    });
    const ver = await api("POST", `/api/rag/sources/${src.id}/versions`, {
      version: "v1",
      content_type: "text/markdown",
      content,
    });
    await api("POST", `/api/rag/sources/${src.id}/versions/${ver.id}/review`, { decision: "approve", reason: "dev corpus auto-approve" }, REVIEWER);
    const pub = await api("POST", `/api/rag/sources/${src.id}/versions/${ver.id}/publish`, {});
    results.push({ file, sourceId: src.id, versionId: ver.id, jobId: pub.job_id, status: pub.status });
    console.log(`OK  ${file}  source=${src.id} version=${ver.id} job=${pub.job_id}`);
  } catch (err) {
    console.error(`FAIL ${file}: ${err.message}`);
    results.push({ file, error: err.message });
  }
}

console.log("---");
console.log(JSON.stringify(results.filter((r) => !r.error).map((r) => r.jobId), null, 0));
