// Read-only gateway smoke test. Requires an operator-provided session cookie;
// it never logs the cookie or writes business data.
const base = (process.env.AI_SMOKE_BASE_URL || "http://localhost:8082").replace(/\/$/, "")
const cookie = process.env.AI_SMOKE_COOKIE
if (!cookie) {
  console.error("AI_SMOKE_COOKIE is required (use a short-lived test session)")
  process.exit(1)
}

const runId = `smoke-${crypto.randomUUID()}`
const response = await fetch(`${base}/api/ai/agent`, {
  method: "POST",
  headers: {
    "content-type": "application/json",
    cookie,
    "x-request-id": runId,
  },
  body: JSON.stringify({
    threadId: `smoke-thread-${crypto.randomUUID()}`,
    runId,
    messages: [{ role: "user", content: process.env.AI_SMOKE_QUERY || "Tìm chính sách hiện hành và nêu nguồn." }],
  }),
})

if (!response.ok) {
  console.error(`gateway returned HTTP ${response.status}`)
  process.exit(2)
}

const text = await response.text()
const events = [...text.matchAll(/^data: (.+)$/gm)].map((match) => {
  try { return JSON.parse(match[1]) } catch { return null }
}).filter(Boolean)
const terminal = events.filter((event) => event.type === "RUN_FINISHED" || event.type === "RUN_ERROR")
if (terminal.length !== 1) {
  console.error(`expected exactly one terminal event, got ${terminal.length}`)
  process.exit(3)
}
if (terminal[0].type === "RUN_ERROR") {
  console.error(`AI run failed: ${terminal[0].error || terminal[0].message || "unknown"}`)
  process.exit(4)
}

const sequences = events.map((event) => event.sequence).filter((value) => Number.isInteger(value))
for (let i = 1; i < sequences.length; i++) {
  if (sequences[i] <= sequences[i - 1]) {
    console.error("event sequence is not strictly increasing")
    process.exit(5)
  }
}
console.log(JSON.stringify({ ok: true, events: events.length, terminal: terminal[0].type, runId }))
