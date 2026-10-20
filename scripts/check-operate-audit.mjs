// Operate (workflow-service admin runtime) mutations change a running case
// outside the maker/checker path. Each one must leave an audit row on the case
// timeline, and the guards that keep human channels away from service-owned
// and approval-routing process variables must stay wired:
//
//   - every mutating Operate* handler in apps/workflow-service/internal/handler
//     (one that accepts POST/PUT/PATCH/DELETE) calls h.recordOperateAudit or
//     h.cancelOpenCase, unless it is listed in the ratchet baseline
//     scripts/check-operate-audit-baseline.json (pre-existing debt; entries
//     that become audited must be removed in the same change);
//   - the handlers in `required` below always carry their guard calls and can
//     never be baselined.
//
//   node scripts/check-operate-audit.mjs           check
//   node scripts/check-operate-audit.mjs --print   print unaudited mutating handlers
import { readFile, readdir } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const handlerDir = join(root, "apps/workflow-service/internal/handler")
const baselinePath = join(root, "scripts/check-operate-audit-baseline.json")

const auditCalls = ["h.recordOperateAudit(", "h.cancelOpenCase("]
const mutatingMethod = /http\.Method(Post|Put|Patch|Delete)\b/

// Guards that must stay in place (AP-PRM-03, AP-AUD-02).
const required = {
  OperateSetInstanceVariables: [
    "h.recordOperateAudit(",
    "service.ProtectedCaseVariableKeys(",
    "elementInstanceInProcess(",
  ],
  OperateCancelInstance: ["h.cancelOpenCase(", "repository.IsTerminalCaseStatus("],
  CompleteUserTask: ["taskCompletionVariables("],
}

async function handlerBodies() {
  const bodies = new Map()
  for (const name of (await readdir(handlerDir)).sort()) {
    if (!name.endsWith(".go") || name.endsWith("_test.go")) continue
    const source = (await readFile(join(handlerDir, name), "utf8")).replace(/\r\n/g, "\n")
    const re = /^func \(h \*WorkflowHandler\) ([A-Za-z0-9_]+)\(w http\.ResponseWriter, r \*http\.Request\) \{\n([\s\S]*?)^\}$/gm
    for (const [, fn, body] of source.matchAll(re)) bodies.set(fn, { file: name, body })
  }
  return bodies
}

const bodies = await handlerBodies()
const unaudited = []
for (const [fn, { body }] of bodies) {
  if (!fn.startsWith("Operate") || !mutatingMethod.test(body)) continue
  if (!auditCalls.some((call) => body.includes(call))) unaudited.push(fn)
}
unaudited.sort()

if (process.argv.includes("--print")) {
  console.log(JSON.stringify({ unaudited }, null, 2))
  process.exit(0)
}

const failures = []
for (const [fn, calls] of Object.entries(required)) {
  const entry = bodies.get(fn)
  if (!entry) {
    failures.push(`${fn}: handler not found in apps/workflow-service/internal/handler`)
    continue
  }
  for (const call of calls) {
    if (!entry.body.includes(call)) failures.push(`${entry.file}: ${fn} must call ${call.replace(/\($/, "")}`)
  }
}

const baseline = JSON.parse(await readFile(baselinePath, "utf8"))
if (!Array.isArray(baseline.unaudited) || baseline.unaudited.some((entry) => typeof entry !== "string")) {
  throw new Error(`${baselinePath}: "unaudited" must be an array of strings`)
}
const known = new Set(baseline.unaudited)
for (const fn of Object.keys(required)) {
  if (known.has(fn)) failures.push(`${fn} is a required audited handler and cannot be baselined`)
}
for (const fn of unaudited) {
  if (!known.has(fn)) {
    failures.push(`${bodies.get(fn).file}: mutating ${fn} writes no audit row (call h.recordOperateAudit)`)
  }
}
const resolved = baseline.unaudited.filter((fn) => !unaudited.includes(fn))
for (const fn of resolved) failures.push(`${fn} is audited now or gone: remove it from ${baselinePath}`)

if (failures.length) {
  console.error("Operate audit gate failed:")
  for (const line of failures) console.error(`  - ${line}`)
  process.exit(1)
}
console.log(
  `Operate audit gate OK: ${Object.keys(required).length} guarded handlers, ` +
    `${baseline.unaudited.length} baselined unaudited mutating handler(s)`,
)
