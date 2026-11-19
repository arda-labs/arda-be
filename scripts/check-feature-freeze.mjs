// Temporary feature freeze (Linear HOA-8): no new features until GĐ1 is done
// (the LNM.201.02 pilot of the spec → test → code pipeline). Arda's logic has
// drifted from the business baseline; work added now would be redone.
//
// Compares the PR tree with the base branch (origin/main) and fails when the
// change ADDS:
//   - a new service directory under apps/
//   - a .bpmn file (no new Zeebe processes: the engine is being replaced)
//   - a scripts/check-*.mjs gate (effort goes to behaviour tests instead)
//   - a file under apps/ai-service/ (no ai-service expansion)
// Modifying existing files is never blocked: bug fixes, security work and the
// GĐ0/GĐ1 issues go through.
//
// Exception: label the PR `freeze-exception` (and say why in the PR), then
// re-run the job. Locally: FREEZE_EXCEPTION=1.
// Lifting the freeze: set FREEZE.active = false in a PR that links HOA-8.
//
//   node scripts/check-feature-freeze.mjs [--base origin/main]

import { execFileSync } from "node:child_process"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { fileURLToPath } from "node:url"

export const FREEZE = {
  active: true,
  issue: "HOA-8",
  until: "GĐ1 xong (làm thử LNM.201.02)",
  label: "freeze-exception",
}

// Files this freeze itself adds; they must not trip their own rule.
const SELF = new Set(["scripts/check-feature-freeze.mjs", "scripts/check-feature-freeze.test.mjs"])

const serviceOf = (path) => {
  const m = /^apps\/([^/]+)\//.exec(path)
  return m ? m[1] : null
}

/**
 * @param {string[]} added        paths added by the change (relative, forward slashes)
 * @param {Set<string>} baseApps  service directory names that exist on the base branch
 * @returns {string[]} violations, one line each
 */
export function findViolations(added, baseApps) {
  const violations = []
  const newServices = new Set()
  for (const path of added) {
    if (SELF.has(path)) continue
    const service = serviceOf(path)
    if (service && !baseApps.has(service)) {
      newServices.add(service)
      continue
    }
    if (path.endsWith(".bpmn")) {
      violations.push(`${path}: new BPMN process (Zeebe is being replaced; no new processes during the freeze)`)
    } else if (/^scripts\/check-[^/]+\.mjs$/.test(path)) {
      violations.push(`${path}: new check script (put the effort into behaviour tests during the freeze)`)
    } else if (service === "ai-service") {
      violations.push(`${path}: new ai-service file (no ai-service expansion during the freeze)`)
    }
  }
  for (const service of [...newServices].sort()) {
    violations.unshift(`apps/${service}/: new service (no new services during the freeze)`)
  }
  return violations
}

export function hasExceptionLabel(event, label = FREEZE.label) {
  const labels = event?.pull_request?.labels ?? []
  return labels.some((l) => (typeof l === "string" ? l : l?.name) === label)
}

function git(args) {
  return execFileSync("git", args, { encoding: "utf8" })
}

function main() {
  if (!FREEZE.active) {
    console.log(`Feature freeze (${FREEZE.issue}) is lifted; nothing to check.`)
    return
  }
  const baseIndex = process.argv.indexOf("--base")
  const base = baseIndex > 0 ? process.argv[baseIndex + 1] : "origin/main"
  try {
    git(["rev-parse", "--verify", "--quiet", `${base}^{commit}`])
  } catch {
    console.error(`${base} is not available; run git fetch origin main first`)
    process.exit(2)
  }

  const added = git(["diff", "--name-only", "--diff-filter=A", "-M", base, "HEAD"])
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
  const baseApps = new Set(
    git(["ls-tree", "-d", "--name-only", `${base}:apps`])
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean),
  )

  const violations = findViolations(added, baseApps)
  if (violations.length === 0) {
    console.log(`Feature freeze (${FREEZE.issue}) OK: ${added.length} added file(s), none adds a feature surface`)
    return
  }

  let event = null
  if (process.env.GITHUB_EVENT_PATH) {
    try {
      event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, "utf8"))
    } catch {
      event = null
    }
  }
  if (process.env.FREEZE_EXCEPTION === "1" || hasExceptionLabel(event)) {
    console.log(`Feature freeze (${FREEZE.issue}): exception granted (label ${FREEZE.label}) for:`)
    for (const v of violations) console.log(`  ${v}`)
    return
  }

  console.error(`Feature freeze (${FREEZE.issue}, until ${FREEZE.until}) blocks this change:`)
  for (const v of violations) console.error(`  ${v}`)
  console.error(`If this is a fix, security work or a GĐ0/GĐ1 issue, add the PR label "${FREEZE.label}", explain why, and re-run.`)
  process.exit(1)
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (isMain) main()
