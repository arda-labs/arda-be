import { readFile } from "node:fs/promises"

const registry = JSON.parse(await readFile(new URL("../contracts/events/arda-events-v1.json", import.meta.url), "utf8"))
const source = await readFile(new URL("../libs/go/arda-events/events.go", import.meta.url), "utf8")
const subjects = [...source.matchAll(/Subject\w+\s*=\s*"([^"]+)"/g)].map(([, value]) => value)
const eventCodes = [...source.matchAll(/Event\w+\s*=\s*"([^"]+)"/g)].map(([, value]) => value)
const registeredSubjects = registry.events.map((event) => event.subject)
const registeredCodes = registry.events.map((event) => event.event_code)

if (registry.version !== "1") throw new Error("event registry version must be 1")
for (const required of ["id", "event_code", "schema_version", "occurred_at", "source_service", "payload"]) {
  if (!registry.envelope.required.includes(required)) throw new Error(`missing envelope field: ${required}`)
}
if (subjects.length !== registeredSubjects.length || subjects.some((value) => !registeredSubjects.includes(value))) {
  throw new Error("event subject registry does not match arda-events.go")
}
if (eventCodes.length !== registeredCodes.length || eventCodes.some((value) => !registeredCodes.includes(value))) {
  throw new Error("event code registry does not match arda-events.go")
}
const STATUSES = new Set(["draft", "wired"])
for (const event of registry.events) {
  if (!event.producer || !event.consumer_status || event.schema_version !== 1) {
    throw new Error(`incomplete event registry entry: ${event.subject}`)
  }
  if (!STATUSES.has(event.status)) {
    throw new Error(`event ${event.subject}: status must be "draft" or "wired"`)
  }
}
const draftCount = registry.events.filter((event) => event.status === "draft").length
console.log(
  `Event registry OK: ${registry.events.length} version-1 events (${draftCount} draft, ${registry.events.length - draftCount} wired)`
)
