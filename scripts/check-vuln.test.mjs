// Negative tests for the allowlist half of scripts/check-vuln.mjs.
//
// The gate shells out to govulncheck across 34 modules, which is far too slow to
// run six times, so these drive the `--allowlist-only` path. That path shares
// the allowlist parsing, the staleness rule and the exit codes with the full
// scan, so what is verified here is the part that decides pass or fail. The
// full scan runs once in CI.
//
//   node scripts/check-vuln.test.mjs

import { readFile, writeFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { execFileSync } from "node:child_process"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const target = join(root, ".github/vuln-allowlist.json")

const run = () => {
  try {
    const out = execFileSync("node", ["scripts/check-vuln.mjs", "--allowlist-only"], {
      cwd: root,
      encoding: "utf8",
      stdio: "pipe",
    })
    return { code: 0, out }
  } catch (e) {
    return { code: e.status ?? 1, out: `${e.stdout ?? ""}${e.stderr ?? ""}` }
  }
}

let failures = 0
function expect(name, cond, detail = "") {
  if (cond) console.log(`  PASS  ${name}`)
  else {
    failures += 1
    console.log(`  FAIL  ${name}${detail ? `\n${detail.trim()}` : ""}`)
  }
}

const original = await readFile(target, "utf8")
const base = JSON.parse(original.replace(/^﻿/, ""))

try {
  console.log("check-vuln: allowlist negative tests")

  const baseline = run()
  expect("the committed allowlist passes", baseline.code === 0, baseline.out)

  // A stale exception must fail: that is what stops an accepted risk from
  // quietly becoming permanent.
  const expired = structuredClone(base)
  expired.accepted[0].reviewBy = "2020-01-01"
  await writeFile(target, JSON.stringify(expired, null, 2), "utf8")
  const stale = run()
  expect(
    "an expired reviewBy fails",
    stale.code === 1 && /has passed/.test(stale.out),
    stale.out,
  )

  // An exception with no review date cannot be re-evaluated later.
  const undated = structuredClone(base)
  delete undated.accepted[0].reviewBy
  await writeFile(target, JSON.stringify(undated, null, 2), "utf8")
  const noDate = run()
  expect("an entry without reviewBy fails", noDate.code === 1 && /reviewBy/.test(noDate.out), noDate.out)

  // A malformed file must produce a security message, not a JSON syntax error.
  await writeFile(target, "{ not json", "utf8")
  const broken = run()
  expect(
    "a malformed allowlist is reported clearly",
    broken.code === 1 && /is not valid JSON/.test(broken.out),
    broken.out,
  )

  // Edited on Windows as often as Linux, so a BOM must not be fatal.
  await writeFile(target, `﻿${original}`, "utf8")
  const bom = run()
  expect("a BOM is tolerated", bom.code === 0, bom.out)

  // The accepted entry must document why it is accepted and what compensates
  // for it, or the exception is not auditable.
  for (const field of ["impact", "compensatingControls", "revisitWhen"]) {
    const bare = structuredClone(base)
    delete bare.accepted[0][field]
    await writeFile(target, JSON.stringify(bare, null, 2), "utf8")
    const r = run()
    expect(
      `an entry missing "${field}" fails`,
      r.code === 1 && new RegExp(field).test(r.out),
      r.out,
    )
  }
} finally {
  await writeFile(target, original, "utf8")
}

const restored = run()
expect("the allowlist is restored and passing", restored.code === 0, restored.out)

console.log(failures === 0 ? "\ncheck-vuln: all negative tests passed" : `\ncheck-vuln: ${failures} failure(s)`)
process.exit(failures === 0 ? 0 : 1)
