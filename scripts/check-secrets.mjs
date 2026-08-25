import { execFile } from "node:child_process"
import { promisify } from "node:util"
import { readFile } from "node:fs/promises"

const execFileAsync = promisify(execFile)
const { stdout } = await execFileAsync("git", ["ls-files", "-z"], { encoding: "utf8" })
const files = stdout.split("\0").filter(Boolean)
const findings = []
const forbidden = [
  /\badmin123\b/i,
  /\b123456\b/,
  new RegExp(["super-secret", "dev-key-change-in-production"].join("-"), "i"),
  new RegExp(["auth", "gateway-secret"].join("-"), "i"),
]
const credentialedUrl = /(?:postgres(?:ql)?|redis):\/\/[^\s"'`<>$]+:[^\s"'`<>$@]+@/i

for (const file of files) {
  if (file.endsWith(".lock") || file.includes("node_modules/")) continue
  let content
  try {
    content = await readFile(file, "utf8")
  } catch (error) {
    if (error?.code === "ENOENT") continue
    throw error
  }
  for (const pattern of forbidden) {
    if (pattern.test(content)) findings.push(`${file}: forbidden credential pattern ${pattern}`)
  }
  if (credentialedUrl.test(content)) findings.push(`${file}: credentialed database/cache URL must come from a secret reference`)
}

if (findings.length) {
  console.error(findings.join("\n"))
  process.exit(1)
}
console.log(`Secret hygiene OK: scanned ${files.length} tracked files`)
