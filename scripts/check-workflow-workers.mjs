// ADR-011: workflow-service is the Zeebe facade, not a home for domain logic.
// A new approval flow runs on the shared common-maker-checker process and its
// owner service implements arda.common.v1.CaseCommandService; workflow-service
// then needs no new code for it.
//
// This gate is a ratchet over the domain-specific worker surface that existed
// before that rule. It fails when workflow-service gains
//   - a new *_workers.go file in internal/worker,
//   - a new domain gRPC client import (libs/go/arda-grpc/client/<domain>), or
//   - a new job topic literal in RegisteredJobTopics.
// Entries that disappear are reported as resolved; remove them from the
// baseline in the same change so the ratchet tightens.
//
//   node scripts/check-workflow-workers.mjs           check
//   node scripts/check-workflow-workers.mjs --print   print the current surface
import { readFile, readdir } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const serviceRoot = join(root, "apps/workflow-service")
const workerDir = join(serviceRoot, "internal/worker")
const baselinePath = join(root, "scripts/check-workflow-workers-baseline.json")

// Generic transport and shared infrastructure: always allowed.
const allowedWorkerFiles = new Set(["case_command_workers.go", "maker_checker_workers.go"])
const allowedClients = new Set(["casecommand", "retry"])

async function* goFiles(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) yield* goFiles(path)
    else if (entry.isFile() && entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) yield path
  }
}

async function currentSurface() {
  const workerFiles = (await readdir(workerDir))
    .filter((name) => name.endsWith("_workers.go") && !allowedWorkerFiles.has(name))
    .sort()

  const clients = new Set()
  for await (const file of goFiles(serviceRoot)) {
    const source = await readFile(file, "utf8")
    for (const [, name] of source.matchAll(/"github\.com\/arda-labs\/arda\/libs\/go\/arda-grpc\/client\/([a-z0-9_-]+)"/g)) {
      if (!allowedClients.has(name)) clients.add(name)
    }
  }

  const topicsSource = await readFile(join(workerDir, "registered_topics.go"), "utf8")
  const block = topicsSource.match(/var RegisteredJobTopics = \[\]string\{([\s\S]*?)\n\}/)
  if (!block) throw new Error("internal/worker/registered_topics.go: RegisteredJobTopics not found")
  const topics = [...block[1].matchAll(/"([^"]+)"/g)].map(([, value]) => value).sort()

  return { workerFiles, clients: [...clients].sort(), topics }
}

const current = await currentSurface()
if (process.argv.includes("--print")) {
  console.log(JSON.stringify(current, null, 2))
  process.exit(0)
}

const baseline = JSON.parse(await readFile(baselinePath, "utf8"))
for (const key of ["workerFiles", "clients", "topics"]) {
  if (!Array.isArray(baseline[key]) || baseline[key].some((entry) => typeof entry !== "string")) {
    throw new Error(`${baselinePath}: "${key}" must be an array of strings`)
  }
  if (new Set(baseline[key]).size !== baseline[key].length) {
    throw new Error(`${baselinePath}: "${key}" contains duplicates`)
  }
}

const labels = {
  workerFiles: "worker file(s) in apps/workflow-service/internal/worker",
  clients: "domain gRPC client import(s) in workflow-service",
  topics: "job topic(s) in RegisteredJobTopics",
}
const added = []
const resolved = []
for (const key of Object.keys(labels)) {
  const known = new Set(baseline[key])
  const now = new Set(current[key])
  for (const value of current[key]) if (!known.has(value)) added.push(`${labels[key]}: ${value}`)
  for (const value of baseline[key]) if (!now.has(value)) resolved.push(`${labels[key]}: ${value}`)
}

if (added.length) {
  console.error("workflow-service gained domain-specific worker surface (ADR-011):")
  for (const line of added) console.error(`  + ${line}`)
  console.error(
    "\nRun the flow on common-maker-checker: register the operation type with owner_service and worker_kind, " +
      "and implement arda.common.v1.CaseCommandService in the owner service. " +
      "See docs/refactor-program/adr-011-workflow-domain-commands.md.",
  )
  process.exit(1)
}

console.log(
  `Workflow worker ratchet OK: ${current.workerFiles.length} legacy worker files, ${current.clients.length} domain clients, ` +
    `${current.topics.length} legacy topics; ${resolved.length} baseline item(s) resolved`,
)
for (const line of resolved) console.log(`  resolved, remove from baseline: ${line}`)
