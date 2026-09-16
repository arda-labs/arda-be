import { readFileSync, readdirSync } from "node:fs"
import { join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"

/**
 * AI deadline-budget gate (ADR-004).
 *
 * The Code Mode sandbox wall clock follows the caller's deadline (the
 * `execute` meta-tool definition), with DefaultExecutionTimeout as the
 * no-deadline fallback and MaxExecutionTimeout as the hard cap. Historically
 * the layers were unrelated: a 3s sandbox, a 4s meta-tool, and a 10s inner LLM
 * call, which made arda.knowledge.search fail with ai.sandbox_timeout for
 * weeks because an external embedding round-trip cannot fit 3s.
 *
 * This check parses the actual Go constants and fails when:
 *   R1. a catalog entry (or an in-catalog client) timeout exceeds the caller
 *       ceiling — the `execute` meta-tool timeout that the sandbox inherits;
 *   R2. the optional-stage budget of a multi-stage tool does not fit that
 *       ceiling together with the minimum continuation budget;
 *   R4. the fallback default or the hard cap is inconsistent with the caller
 *       ceiling (default ≤ execute ≤ max).
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

const timeoutRe = /Timeout:\s+(\d+)\s*\*\s*time\.(Second|Millisecond)/g

// --- Sandbox fallback + hard cap -------------------------------------------

const engineRel = "apps/ai-service/internal/sandbox/engine.go"
const engine = read(engineRel)
const defaultMatch = engine.match(
  /DefaultExecutionTimeout\s*=\s*(\d+)\s*\*\s*time\.(Second|Millisecond)/
)
const maxMatch = engine.match(
  /MaxExecutionTimeout\s*=\s*(\d+)\s*\*\s*time\.(Second|Millisecond)/
)
if (!defaultMatch || !maxMatch) {
  console.error(`${engineRel}: Default/MaxExecutionTimeout not found`)
  process.exit(1)
}
const defaultMs = durationToMs(defaultMatch[1], defaultMatch[2])
const maxMs = durationToMs(maxMatch[1], maxMatch[2])

// --- Caller ceiling: the execute meta-tool deadline -------------------------

const toolsDir = join(aiRoot, "internal", "tools")
const metaFiles = readdirSync(toolsDir)
  .filter((name) => name.startsWith("meta_") && name.endsWith(".go") && !name.endsWith("_test.go"))
  .sort()

let executeMs = 0
for (const name of metaFiles) {
  const rel = relative(root, join(toolsDir, name)).replaceAll("\\", "/")
  const content = readFileSync(join(toolsDir, name), "utf8")
  for (const item of collect(content, timeoutRe, rel)) {
    if (rel.endsWith("meta_execute.go")) {
      executeMs = Math.max(executeMs, item.ms)
    }
  }
}
if (executeMs <= 0) {
  console.error(`${toolsDir}/meta_execute.go: execute timeout not found`)
  process.exit(1)
}
if (defaultMs > executeMs) {
  violations.push(
    `${engineRel} — DefaultExecutionTimeout ${defaultMs}ms exceeds the execute ceiling ${executeMs}ms; the sandbox would outlive its caller (ADR-004 R4)`
  )
}
if (maxMs < executeMs) {
  violations.push(
    `${engineRel} — MaxExecutionTimeout ${maxMs}ms is below the execute ceiling ${executeMs}ms; the hard cap would cut legitimate calls (ADR-004 R4)`
  )
}

// --- R1: catalog entries and in-catalog clients ----------------------------

const catalogDir = join(aiRoot, "internal", "catalog")
const catalogFiles = readdirSync(catalogDir)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .sort()

const entries = []
for (const name of catalogFiles) {
  const rel = relative(root, join(catalogDir, name)).replaceAll("\\", "/")
  const content = readFileSync(join(catalogDir, name), "utf8")
  for (const item of collect(content, timeoutRe, rel)) {
    entries.push(item)
    if (item.ms > executeMs) {
      violations.push(
        `${item.file}:${item.line} — timeout ${item.ms}ms exceeds the execute ceiling ${executeMs}ms (ADR-004 R1)`
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
if (rewriteBudget >= executeMs) {
  violations.push(
    `${serviceRel} — rewriteBudget ${rewriteBudget}ms must be strictly inside the ${executeMs}ms execute ceiling (ADR-004 R2)`
  )
}
if (rewriteBudget + variantMinBudget > executeMs) {
  violations.push(
    `${serviceRel} — rewriteBudget ${rewriteBudget}ms + variantMinBudget ${variantMinBudget}ms exceeds the ${executeMs}ms ceiling; a variant could not start after the rewrite (ADR-004 R2)`
  )
}

// --- Report ----------------------------------------------------------------

const worstEntry = entries.reduce((max, item) => Math.max(max, item.ms), 0)
notes.push(`sandbox default ${defaultMs}ms, hard cap ${maxMs}ms`)
notes.push(`caller ceiling (execute) ${executeMs}ms`)
notes.push(`catalog timeouts ${entries.length} (max ${worstEntry}ms)`)
notes.push(`rewrite budget ${rewriteBudget}ms, variant floor ${variantMinBudget}ms`)

if (violations.length > 0) {
  const message = [
    ...violations,
    "",
    "Interactive SDK calls must fit the caller deadline the sandbox inherits (docs/ai/adr-004-budget-and-error-contract.md).",
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
