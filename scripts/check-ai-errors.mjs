import { readFileSync, readdirSync, statSync } from "node:fs"
import { join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"

/**
 * AI error-contract gate (ADR-004 §2).
 *
 * Every failure that can reach an HTTP response or the sandbox boundary must
 * carry a stable machine-readable code. Historically codes were invented per
 * call site and sandbox failures were audited as SUCCEEDED with an empty
 * error_code, so operators and dashboards could not see what actually failed.
 *
 * Fails when:
 *   R8. an `ai.*` code passed to problem(w, status, code) has no
 *       docs/problems/<code>.md page;
 *   R7. a sandbox error code (tools.SandboxError) or the generic tool-code
 *       fallback is missing from the ADR-004 taxonomy table.
 *
 * Orphan problem pages (a page whose code no longer appears anywhere in the
 * service) are printed as warnings: retired surfaces are tracked in
 * docs/ai/audit-2026-09.md item A5.
 */

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const aiRoot = join(root, "apps", "ai-service")
const problemsDir = join(root, "docs", "problems")
const adrRel = "docs/ai/adr-004-budget-and-error-contract.md"

const violations = []
const warnings = []

function walk(dir) {
  const out = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (["node_modules", ".git", "vendor"].includes(entry.name)) continue
    const full = join(dir, entry.name)
    if (entry.isDirectory()) out.push(...walk(full))
    else if (entry.isFile() && entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) {
      out.push(full)
    }
  }
  return out
}

const files = walk(aiRoot).sort()
const literals = new Set()
const apiCodes = new Map()

const literalRe = /"(ai\.[a-z0-9_]+(?:\.[a-z0-9_]+)*)"/g
const problemRe = /problem\([^,]+,[^,]+,\s*"([^"]+)"/g

for (const file of files) {
  const rel = relative(root, file).replaceAll("\\", "/")
  const content = readFileSync(file, "utf8")
  for (const match of content.matchAll(literalRe)) {
    literals.add(match[1])
  }
  for (const match of content.matchAll(problemRe)) {
    if (!apiCodes.has(match[1])) {
      apiCodes.set(match[1], rel)
    }
  }
}

// --- R8: every API-facing code has a problem page --------------------------

for (const [code, rel] of [...apiCodes.entries()].sort()) {
  const page = join(problemsDir, `${code}.md`)
  try {
    statSync(page)
  } catch {
    violations.push(
      `${rel} — "${code}" has no docs/problems/${code}.md page (ADR-004 R8)`
    )
  }
}

// --- R7: sandbox codes live in the taxonomy ---------------------------------

const taxonomy = readFileSync(join(root, adrRel), "utf8")
const sandboxEngine = readFileSync(
  join(aiRoot, "internal", "sandbox", "engine.go"),
  "utf8"
)
const sandboxCodes = new Set()
for (const match of sandboxEngine.matchAll(/Code:\s*"([^"]+)"/g)) {
  sandboxCodes.add(match[1])
}
const toolsTypes = readFileSync(join(aiRoot, "internal", "tools", "types.go"), "utf8")
const fallback = toolsTypes.match(/return\s+"(ai\.[^"]+)"/)
if (fallback) {
  sandboxCodes.add(fallback[1])
}

for (const code of [...sandboxCodes].sort()) {
  if (!taxonomy.includes(`\`${code}\``)) {
    violations.push(
      `${adrRel} — sandbox code "${code}" is missing from the taxonomy table (ADR-004 R7)`
    )
  }
}

// --- Orphan pages (warning only) -------------------------------------------

for (const name of readdirSync(problemsDir)) {
  if (!name.startsWith("ai.") || !name.endsWith(".md")) continue
  const code = name.slice(0, -3)
  if (!literals.has(code)) {
    warnings.push(`docs/problems/${name} — "${code}" no longer appears in ai-service`)
  }
}

// --- Report ----------------------------------------------------------------

if (warnings.length > 0) {
  console.log(
    `AI error-contract: ${warnings.length} orphan problem page(s) (retired surface, see audit A5):`
  )
  for (const warning of warnings) {
    console.log(`  [orphan] ${warning}`)
  }
}

if (violations.length > 0) {
  console.error(
    [
      ...violations,
      "",
      "Codes are defined in docs/ai/adr-004-budget-and-error-contract.md §2.",
      "Add the missing problem page or taxonomy row before merging.",
    ].join("\n")
  )
  process.exit(1)
}

console.log(
  `AI error-contract invariant OK (${apiCodes.size} API codes with pages, ${sandboxCodes.size} sandbox codes in the taxonomy)`
)
