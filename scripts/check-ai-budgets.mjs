import { readFileSync, readdirSync, statSync } from "node:fs"
import { join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"

/**
 * AI deadline-budget gate (ADR-004).
 *
 * The Code Mode sandbox wall clock is the authoritative ceiling for any
 * interactive SDK call. Historically five independent timeout layers existed
 * and nothing connected them; arda.knowledge.search died for weeks with
 * ai.sandbox_timeout because an inner 10s LLM call could not fit a 3s sandbox.
 *
 * This check parses the actual Go constants and fails when:
 *   R1. a catalog entry (or in-catalog client) timeout exceeds the ceiling;
 *   R2. the optional-stage budget of a multi-stage tool does not fit the
 *       ceiling together with the minimum continuation budget;
 *   R4. the `execute` meta-tool timeout is below the ceiling (the sandbox must
 *       report its own timeout before the handler does).
 *
 * Run with --report to print violations without failing.
 */

const isReport = process.argv.includes("--report")
const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const aiRoot = join(root, "apps", "ai-service")

const violations = []
const notes = []

function read(rel) {
  return readFileSync(join(root, rel), "utf8")
}

function lineOf(content, index) {
  return content.slice(0, index).split("\n").length
}

function durationToMs(value, unit) {
  return unit === "Second" ? value * 1000 : Number(value)
}

function collect(content, regex, rel) {
  const out = []
  for (const match of content.matchAll(regex)) {
    out.push({
      file: rel,
      line: lineOf(content, match.index ?? 0),
      ms: durationToMs(match[1], match[2]),
      name: match[3] ?? "",
    })
  }
  return out
}

// --- Sandbox ceiling -------------------------------------------------------

const engineRel = "apps/ai-service/internal/sandbox/engine.go"
const engine = read(engineRel)
const ceilingMatch = engine.match(
  /DefaultExecutionTimeout\s*=\s*(\d+)\s*\*\s*time\.(Second|Millisecond)/
)
if (!ceilingMatch) {
  console.error(`${engineRel}: DefaultExecutionTimeout not found`)
  process.exit(1)
}
const ceilingMs = durationToMs(ceilingMatch[1], ceilingMatch[2])

// --- R1: catalog entries and in-catalog clients ----------------------------

const catalogDir = join(aiRoot, "internal", "catalog")
const catalogFiles = readdirSync(catalogDir)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .sort()

const timeoutRe = /Timeout:\s+(\d+)\s*\*\s*time\.(Second|Millisecond)/g
const entries = []
for (const name of catalogFiles) {
  const rel = relative(root, join(catalogDir, name)).replaceAll("\\", "/")
  const content = readFileSync(join(catalogDir, name), "utf8")
  for (const item of collect(content, timeoutRe, rel)) {
    entries.push(item)
    if (item.ms > ceilingMs) {
      violations.push(
        `${item.file}:${item.line} — timeout ${item.ms}ms exceeds the sandbox ceiling ${ceilingMs}ms (ADR-004 R1)`
      )
    }
  }
}

// --- R4: meta-tool timeouts ------------------------------------------------

const toolsDir = join(aiRoot, "internal", "tools")
const metaFiles = readdirSync(toolsDir)
  .filter((name) => name.startsWith("meta_") && name.endsWith(".go") && !name.endsWith("_test.go"))
  .sort()

for (const name of metaFiles) {
  const rel = relative(root, join(toolsDir, name)).replaceAll("\\", "/")
  const content = readFileSync(join(toolsDir, name), "utf8")
  for (const item of collect(content, timeoutRe, rel)) {
    if (rel.endsWith("meta_execute.go") && item.ms < ceilingMs) {
      violations.push(
        `${item.file}:${item.line} — execute timeout ${item.ms}ms is below the sandbox ceiling ${ceilingMs}ms; the handler would win the race and mask the sandbox error (ADR-004 R4)`
      )
    }
  }
}

// --- R2: multi-stage optional budget --------------------------------------

const serviceRel = "apps/ai-service/internal/knowledge/service.go"
const service = read(serviceRel)
const budgetRe = /(rewriteBudget|variantMinBudget)\s*=\s*(\d+)\s*\*\s*time\.(Second|Millisecond)/g
const budgets = {}
for (const match of service.matchAll(budgetRe)) {
  budgets[match[1]] = durationToMs(match[2], match[3])
}
for (const name of ["rewriteBudget", "variantMinBudget"]) {
  if (budgets[name] === undefined) {
    console.error(`${serviceRel}: ${name} not found`)
    process.exit(1)
  }
}
const { rewriteBudget, variantMinBudget } = budgets
if (rewriteBudget >= ceilingMs) {
  violations.push(
    `${serviceRel} — rewriteBudget ${rewriteBudget}ms must be strictly inside the ${ceilingMs}ms ceiling (ADR-004 R2)`
  )
}
if (rewriteBudget + variantMinBudget > ceilingMs) {
  violations.push(
    `${serviceRel} — rewriteBudget ${rewriteBudget}ms + variantMinBudget ${variantMinBudget}ms exceeds the ${ceilingMs}ms ceiling; a variant could not start after the rewrite (ADR-004 R2)`
  )
}

// --- Report ----------------------------------------------------------------

const worstEntry = entries.reduce((max, item) => Math.max(max, item.ms), 0)
notes.push(`sandbox ceiling ${ceilingMs}ms`)
notes.push(`catalog timeouts ${entries.length} (max ${worstEntry}ms)`)
notes.push(`rewrite budget ${rewriteBudget}ms, variant floor ${variantMinBudget}ms`)

if (violations.length > 0) {
  const message = [
    ...violations,
    "",
    "Interactive SDK calls must fit the sandbox wall clock (docs/ai/adr-004-budget-and-error-contract.md).",
    isReport
      ? "(report mode — fix the budget or file an ADR exception)"
      : "Fix the budget split or file an ADR exception before merging.",
  ].join("\n")
  if (isReport) {
    console.warn(message)
  } else {
    console.error(message)
    process.exit(1)
  }
}

console.log(`AI budget invariant OK (${notes.join("; ")})`)
