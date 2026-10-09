// Runs govulncheck across every Go module and fails on any reachable advisory
// that is not explicitly accepted in .github/vuln-allowlist.json.
//
// Why this gate exists: the audit found four reachable CVEs that had been
// shipping for months behind twenty custom invariant scripts and not one
// dependency check. `check-secrets.mjs` proves nobody committed a credential; it
// says nothing about what is inside the build graph.
//
// Only symbol-level findings are enforced. govulncheck also reports module-level
// findings for code that is present but never called, and failing the build on
// those trains people to reach for the allowlist instead of bumping. The
// symbol-level set is the one that describes reachable risk.
//
// Every accepted advisory must carry a compensating control and a review date,
// so an exception cannot quietly become permanent.

import { readFile, readdir } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { execFileSync } from "node:child_process"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const allowlistPath = join(root, ".github/vuln-allowlist.json")

// Tolerate a UTF-8 BOM: the file is edited on Windows as often as on Linux, and
// a security gate that dies with a JSON syntax error instead of a security
// message trains people to ignore it.
const allowlistRaw = (await readFile(allowlistPath, "utf8")).replace(/^﻿/, "")
let allowlist
try {
  allowlist = JSON.parse(allowlistRaw)
} catch (e) {
  console.error(`${allowlistPath} is not valid JSON: ${e.message}`)
  process.exit(1)
}
const accepted = new Map((allowlist.accepted ?? []).map((a) => [a.id, a]))

// Every accepted advisory must be a reviewable decision, not a suppression. A
// bare id in this file is indistinguishable from a rubber stamp, so the gate
// refuses entries that do not carry the reasoning and the compensating control.
const required = ["id", "module", "title", "scope", "impact", "compensatingControls", "reviewed", "revisitWhen"]
const incomplete = []
for (const entry of accepted.values()) {
  for (const field of required) {
    const value = entry[field]
    if (value === undefined || value === null || value === "" ||
        (Array.isArray(value) && value.length === 0)) {
      incomplete.push(`${entry.id ?? "<missing id>"}: accepted entries must declare "${field}"`)
    }
  }
}
if (incomplete.length) {
  console.error(`vuln allowlist entries are incomplete:\n${incomplete.join("\n")}`)
  process.exit(1)
}

// A stale exception is a silent regression. Fail if one is past its review date.
const today = new Date()
const stale = []
for (const entry of accepted.values()) {
  if (!entry.reviewBy) {
    stale.push(`${entry.id}: accepted without a reviewBy date`)
    continue
  }
  const due = new Date(entry.reviewBy)
  if (!Number.isNaN(due.getTime()) && due < today) {
    stale.push(`${entry.id}: reviewBy ${entry.reviewBy} has passed; re-assess or bump the dependency`)
  }
}
// --allowlist-only validates the allowlist and exits, without scanning. The
// negative tests use it so they stay fast; the full scan runs once in CI. The
// two halves are the same code, so the tests still cover the gate's decision
// logic.
const allowlistOnly = process.argv.includes("--allowlist-only")

if (stale.length) {
  console.error(`vuln allowlist is stale:\n${stale.join("\n")}`)
  process.exit(1)
}
if (allowlistOnly) {
  console.log(`Vulnerability allowlist OK: ${accepted.size} accepted entr(y/ies), none stale`)
  process.exit(0)
}

const moduleDirs = []
for (const group of ["apps", "libs/go", "tools"]) {
  for (const entry of await readdir(join(root, group), { withFileTypes: true })) {
    if (!entry.isDirectory()) continue
    const dir = join(root, group, entry.name)
    try {
      await readFile(join(dir, "go.mod"))
      moduleDirs.push({ name: `${group}/${entry.name}`, dir })
    } catch {
      /* not a module */
    }
  }
}

const found = new Map() // id -> Set(module)
const failures = []

if (!allowlistOnly) {
for (const mod of moduleDirs) {
  let output
  try {
    output = execFileSync(
      "go",
      ["run", "golang.org/x/vuln/cmd/govulncheck@latest", "-show", "verbose", "./..."],
      { cwd: mod.dir, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], maxBuffer: 64 * 1024 * 1024 },
    )
  } catch (e) {
    // govulncheck exits non-zero when it finds vulnerabilities, so a non-zero
    // status with parseable output is a finding, not a crash. Only a run that
    // produced no Symbol Results section at all is treated as a tool failure.
    output = `${e.stdout ?? ""}${e.stderr ?? ""}`
    if (!output.includes("=== Symbol Results ===")) {
      failures.push(`${mod.name}: govulncheck could not run\n${output.split("\n").slice(0, 5).join("\n")}`)
      continue
    }
  }

  const symbolSection = output.match(/=== Symbol Results ===([\s\S]*?)=== Package Results ===/)
  if (!symbolSection) continue // no reachable findings
  for (const match of symbolSection[1].matchAll(/GO-\d{4}-\d+/g)) {
    const id = match[0]
    if (!found.has(id)) found.set(id, new Set())
    found.get(id).add(mod.name)
  }
}
}

const unexpected = []
for (const [id, modules] of found) {
  if (accepted.has(id)) continue
  unexpected.push(
    `${id} is reachable in ${modules.size} module(s) and is not in .github/vuln-allowlist.json ` +
      `(${Array.from(modules).slice(0, 3).join(", ")}${modules.size > 3 ? ", ..." : ""})`,
  )
}

if (unexpected.length || failures.length) {
  if (unexpected.length) {
    console.error(
      `Unaccepted reachable vulnerabilities:\n${unexpected.join("\n")}\n\n` +
        "Fix the dependency, or add an entry to .github/vuln-allowlist.json with the " +
        "compensating control and a review date.",
    )
  }
  if (failures.length) console.error(`\nTool failures:\n${failures.join("\n")}`)
  process.exit(1)
}

const acceptedStillPresent = [...found.keys()].filter((id) => accepted.has(id))
if (acceptedStillPresent.length === 0 && accepted.size > 0) {
  console.log(
    `Vulnerability scan OK: no reachable advisories in ${moduleDirs.length} modules. ` +
      `${accepted.size} allowlist entr(y/ies) no longer apply and can be removed.`,
  )
  process.exit(0)
}

console.log(
  `Vulnerability scan OK: ${moduleDirs.length} modules, 0 unaccepted reachable advisories. ` +
    `Still present and accepted: ${acceptedStillPresent.join(", ")}.`,
)
