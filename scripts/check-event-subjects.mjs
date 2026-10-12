// Domain event subjects live in one place: libs/go/arda-events/events.go,
// mirrored by contracts/events/arda-events-v1.json (see check-events.mjs).
// Producers and consumers must reference ardaevents.Subject* / Event*
// constants, so a renamed or versioned subject cannot drift between the
// service that publishes it and the services that consume it.
//
// This gate fails when non-test Go code under apps/ or libs/go/ contains a
// string literal shaped like a NATS subject (arda.<a>.<b>.<c>...) outside the
// registry file and the explicit exemptions below.
//
// It also guards the ARDA_EVENTS stream filter (outbox.StreamSubjects): every
// registered subject must be covered, no entry may be the catch-all "arda.>"
// or reach ai-service's AI_EVENTS subjects (arda.ai.*), because JetStream
// refuses overlapping streams and the second service to start would fail.
// When the arda-infra repository sits next to this one, the provisioned
// stream manifest must list exactly the same subjects.
import { access, readdir, readFile } from "node:fs/promises"
import { join, relative, sep } from "node:path"
import { fileURLToPath } from "node:url"

const root = fileURLToPath(new URL("../", import.meta.url))
const registryFile = "libs/go/arda-events/events.go"
const streamFile = "libs/go/arda-events/outbox/outbox.go"
const aiPrefix = "arda.ai."

// Every exemption needs a reason. Keep this list short.
const exemptions = {
  "apps/ai-service/internal/events/":
    "ai-service owns its AI_EVENTS stream (arda.ai.*); contract in docs/ai/nats-events.md",
  "apps/notification-service/internal/service/ops_service.go":
    "operator-facing catalog of subscribable event types; lists planned subjects that no service produces yet",
}

// Three or more lowercase segments after "arda." — excludes AI tool SDK paths
// such as "arda.crm.getCustomer" and "arda.knowledge.search".
const subjectLiteral = /"(arda\.[a-z0-9_]+(?:\.[a-z0-9_>*-]+){2,})"/g

async function* goFiles(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue
    const path = join(dir, entry.name)
    if (entry.isDirectory()) yield* goFiles(path)
    else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) yield path
  }
}

for (const [path, reason] of Object.entries(exemptions)) {
  if (!reason.trim()) throw new Error(`exemption ${path} has no justification`)
}

const violations = []

// Stream filter checks.
const streamSource = await readFile(join(root, streamFile), "utf8")
const streamBlock = streamSource.match(/var StreamSubjects = \[\]string\{([\s\S]*?)\n\}/)
if (!streamBlock) throw new Error(`${streamFile}: StreamSubjects not found`)
const streamSubjects = [...streamBlock[1].matchAll(/"([^"]+)"/g)].map(([, value]) => value)
const covers = (filter, subject) =>
  filter.endsWith(".>") ? subject.startsWith(filter.slice(0, -1)) : filter === subject
for (const filter of streamSubjects) {
  if (!/^arda\.[a-z0-9_]+\.>$/.test(filter)) violations.push(`${streamFile}: stream subject "${filter}" must be one domain prefix like arda.<domain>.>`)
  if (covers(filter, aiPrefix + "x") || aiPrefix.startsWith(filter.slice(0, -1))) {
    violations.push(`${streamFile}: stream subject "${filter}" overlaps AI_EVENTS (${aiPrefix}*)`)
  }
}
const registrySource = await readFile(join(root, registryFile), "utf8")
for (const [, subject] of registrySource.matchAll(/Subject\w+\s*=\s*"([^"]+)"/g)) {
  if (!streamSubjects.some((filter) => covers(filter, subject))) {
    violations.push(`${registryFile}: subject "${subject}" is not captured by ARDA_EVENTS (${streamFile})`)
  }
}
let infraNote = "arda-infra not found next to this repo; manifest comparison skipped"
for (const candidate of ["../arda-infra", "../../arda-infra"]) {
  const manifest = join(root, candidate, "platform/manifests/nats/arda-events-stream.json")
  try { await access(manifest) } catch { continue }
  const provisioned = JSON.parse(await readFile(manifest, "utf8")).subjects ?? []
  const missing = provisioned.filter((value) => !streamSubjects.includes(value))
  const extra = streamSubjects.filter((value) => !provisioned.includes(value))
  if (missing.length || extra.length) {
    violations.push(`${streamFile}: StreamSubjects differ from arda-infra stream manifest (missing: ${missing.join(", ") || "-"}; extra: ${extra.join(", ") || "-"})`)
  }
  infraNote = "matches arda-infra stream manifest"
  break
}

let scanned = 0
for (const group of ["apps", "libs/go"]) {
  for await (const file of goFiles(join(root, group))) {
    const rel = relative(root, file).split(sep).join("/")
    if (rel === registryFile) continue
    if (Object.keys(exemptions).some((prefix) => rel === prefix || (prefix.endsWith("/") && rel.startsWith(prefix)))) continue
    scanned++
    const lines = (await readFile(file, "utf8")).split(/\r?\n/)
    lines.forEach((line, index) => {
      if (line.trimStart().startsWith("//")) return
      if (line.includes('"arda.>"')) violations.push(`${rel}:${index + 1}: catch-all stream subject "arda.>" overlaps AI_EVENTS; use outbox.EnsureStream`)
      for (const [, subject] of line.matchAll(subjectLiteral)) {
        violations.push(`${rel}:${index + 1}: literal subject "${subject}"`)
      }
    })
  }
}

if (violations.length) {
  console.error(violations.join("\n"))
  console.error(
    `\n${violations.length} hard-coded event subject(s). Register the subject in ${registryFile} and ` +
      "contracts/events/arda-events-v1.json, then reference ardaevents.Subject*."
  )
  process.exit(1)
}
console.log(`Event subjects OK: ${scanned} Go files reference subjects through ${registryFile}; ARDA_EVENTS filter covers the registry without arda.ai.* (${infraNote})`)
