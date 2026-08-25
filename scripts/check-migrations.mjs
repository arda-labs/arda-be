import { readdir, readFile } from "node:fs/promises"
import { join } from "node:path"

const roots = ["apps"]
const migrationName = /^\d{14}_[a-z0-9][a-z0-9_-]*\.sql$/
const violations = []

async function walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) await walk(path)
    else if (entry.isFile() && entry.name.endsWith(".sql") && (path.includes("/migrations/") || path.includes("\\migrations\\"))) {
      if (!migrationName.test(entry.name)) violations.push(`${path}: invalid migration filename`)
      const source = await readFile(path, "utf8")
      if (!/^\s*-- \+goose Up\b/m.test(source)) violations.push(`${path}: missing goose Up marker`)
      if (!/^\s*-- \+goose Down\b/m.test(source)) violations.push(`${path}: missing goose Down marker`)
      if (entry.name.startsWith("20260825") && /DROP\s+(TABLE|DATABASE|SCHEMA)\b[^;]*\bCASCADE\b/i.test(source)) {
        violations.push(`${path}: destructive CASCADE drop is not allowed in a migration`)
      }
      // New migrations must not introduce another synthetic tenant fallback.
      if (entry.name.startsWith("20260825") &&
          /tenant_id[\s\S]{0,120}?DEFAULT\s+['"](?:default)?['"]/i.test(source) ||
          entry.name.startsWith("20260825") && /SET\s+DEFAULT\s+['"](?:default)?['"]/i.test(source)) {
        violations.push(`${path}: synthetic or empty tenant default is not allowed in new migrations`)
      }
    }
  }
}

for (const root of roots) await walk(root)
if (violations.length) {
  console.error(violations.join("\n"))
  process.exit(1)
}
console.log("Migration standards OK")
