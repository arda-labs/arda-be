import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const file = "docs/refactor-program/phase-0/rls-pilot.md"
const source = await readFile(resolve(root, file), "utf8")
const required = [
  "SET LOCAL",
  "ENABLE ROW LEVEL SECURITY",
  "FORCE ROW LEVEL SECURITY",
  "USING (tenant_id = current_setting('arda.tenant_id', true))",
  "WITH CHECK (tenant_id = current_setting('arda.tenant_id', true))",
  "historical `default` tenant",
]
const missing = required.filter((value) => !source.includes(value))
if (missing.length) {
  console.error(`${file}: missing RLS pilot controls: ${missing.join(", ")}`)
  process.exit(1)
}

if (/ALTER TABLE\s+iam_|ALTER TABLE\s+crm_|ALTER TABLE\s+hrm_|ALTER TABLE\s+finance_|ALTER TABLE\s+workflow_|ALTER TABLE\s+platform_|ALTER TABLE\s+media_|ALTER TABLE\s+notification_/i.test(source)) {
  console.error(`${file}: production table RLS must not be enabled by the feasibility pilot`)
  process.exit(1)
}

console.log("RLS pilot artifact OK: scratch policy only, production adoption gated")
