import { readdir, readFile } from "node:fs/promises"
import { join, relative, resolve, sep } from "node:path"
import { pathToFileURL } from "node:url"

const root = process.cwd()
const roots = ["apps"]
const mutationPattern = /\b(UPDATE|DELETE\s+FROM)\s+(?:public\.)?(fin_journal_(?:entries|lines))\b/gi

// These repository/service statements implement the only allowed entry
// state transitions: PENDING -> POSTED, PENDING -> VOID, POSTED -> REVERSED.
const allowed = new Map([
  ["apps/finance-service/internal/service/posting_service.go|UPDATE fin_journal_entries", 2],
  ["apps/finance-service/internal/repository/posting_repo.go|UPDATE fin_journal_entries", 1],
])

export function inspectSource(source, file) {
  const counts = new Map()
  if (file.endsWith("_test.go")) return counts
  for (const match of source.matchAll(mutationPattern)) {
    const verb = match[1].toUpperCase().startsWith("DELETE") ? "DELETE FROM" : "UPDATE"
    const table = match[2].toLowerCase()
    const key = `${file}|${verb} ${table}`
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  return counts
}

export function validateCounts(actual) {
  const errors = []
  for (const [key, count] of actual) {
    if (allowed.get(key) !== count) errors.push(`${key}: found ${count} mutation(s), allowlisted ${allowed.get(key) ?? 0}`)
  }
  for (const [key, count] of allowed) {
    if ((actual.get(key) ?? 0) !== count) errors.push(`${key}: expected ${count} allowlisted mutation(s), found ${actual.get(key) ?? 0}`)
  }
  return errors
}

async function walk(dir, actual) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) {
      await walk(path, actual)
    } else if (entry.isFile() && entry.name.endsWith(".go")) {
      const file = relative(root, path).split(sep).join("/")
      for (const [key, count] of inspectSource(await readFile(path, "utf8"), file)) {
        actual.set(key, (actual.get(key) ?? 0) + count)
      }
    }
  }
}

async function main() {
  const actual = new Map()
  for (const dir of roots) await walk(join(root, dir), actual)
  const errors = validateCounts(actual)
  if (errors.length) {
    console.error(errors.join("\n"))
    process.exitCode = 1
    return
  }
  console.log("Journal mutation invariant OK: only allowlisted state transitions write fin_journal_entries")
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await main()
}
