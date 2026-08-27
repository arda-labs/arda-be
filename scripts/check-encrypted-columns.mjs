import { readFile, readdir } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

/**
 * Encrypted-column invariant — BE mirror of the FE invariant scripts.
 *
 * contracts/security/encrypted-columns-v1.json is the single registry of
 * database columns that store secrets. Enforced guarantees:
 *   1. every `handling: encrypted` entry has a real implementation file whose
 *      Go source references libs/go/arda-crypto together with the column name
 *      (the registry cannot silently rot);
 *   2. `allow_plaintext` entries require a justification and stay out of the
 *      crypto requirement;
 *   3. an unregistered secret-like identifier inside repository SQL surfaces
 *      as a warning so new secrets cannot slip in unnoticed (non-fatal).
 */
const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const registry = JSON.parse(
  await readFile(join(root, "contracts", "security", "encrypted-columns-v1.json"), "utf8")
)

if (registry.version !== 1) {
  throw new Error("encrypted-columns registry version must be 1")
}

const SENSITIVE_PATTERN =
  /\b(api_key|apikey|client_secret|access_token|refresh_token|private_key|card_number)\b/gi

async function listGoFiles(dir) {
  let entries
  try {
    entries = await readdir(dir, { withFileTypes: true, recursive: true })
  } catch {
    return []
  }
  return entries.filter((entry) => entry.isFile() && entry.name.endsWith(".go"))
}

const violations = []
const warnings = []
const registeredIdentifiers = new Set()
const registeredImplementations = new Set()

for (const entry of registry.columns ?? []) {
  const serviceRoot = join(root, "apps", entry.service)
  const key = `${entry.service}:${entry.table}.${entry.column}`
  registeredIdentifiers.add(entry.column.toLowerCase())
  if (entry.implementation) {
    registeredImplementations.add(
      join(serviceRoot, entry.implementation).replaceAll("\\", "/")
    )
  }

  if (entry.handling === "allow_plaintext") {
    if (!String(entry.cipher ?? "").trim()) {
      violations.push(`${key}: allow_plaintext requires a justification`)
    }
    continue
  }
  if (entry.handling !== "encrypted") {
    violations.push(`${key}: handling must be "encrypted" or "allow_plaintext"`)
    continue
  }

  if (!entry.implementation) {
    violations.push(`${key}: missing implementation reference`)
    continue
  }
  const implementationPath = join(serviceRoot, entry.implementation)
  let implementationSource
  try {
    implementationSource = await readFile(implementationPath, "utf8")
  } catch {
    violations.push(
      `${key}: implementation file missing: apps/${entry.service}/${entry.implementation}`
    )
    continue
  }

  const lowerSource = implementationSource.toLowerCase()
  if (!lowerSource.includes("ardacrypto.")) {
    violations.push(`${key}: implementation must use libs/go/arda-crypto`)
  }
  if (!lowerSource.includes(entry.column.toLowerCase())) {
    violations.push(`${key}: implementation does not reference the registered column`)
  }
}

// Heuristic sweep over repository layers for secret-like SQL identifiers that
// are not registered. Warnings only: they surface candidates for registration
// or refactor without blocking unrelated work.
const appEntries = await readdir(join(root, "apps"), { withFileTypes: true })
for (const dirEntry of appEntries) {
  if (!dirEntry.isDirectory()) continue
  const repoDir = join(root, "apps", dirEntry.name, "internal", "repository")
  const goFiles = await listGoFiles(repoDir)
  for (const file of goFiles) {
    const absolutePath =
      (file.parentPath ?? repoDir) + "\\" + file.name
    if (registeredImplementations.has(absolutePath.replaceAll("\\", "/"))) {
      // The file is the registered encryption implementation itself; its
      // secret identifiers are intentional and already governed by the crypto
      // requirement above.
      continue
    }
    let source
    try {
      source = await readFile(join(file.parentPath ?? repoDir, file.name), "utf8")
    } catch {
      continue
    }
    if (!/(insert\s+into|update\s+\w+\s+set)/i.test(source)) continue
    const matches = [...source.matchAll(SENSITIVE_PATTERN)]
    if (
      matches.length > 0 &&
      !matches.every((match) => registeredIdentifiers.has(match[0].toLowerCase()))
    ) {
      const relativePath = join(file.parentPath ?? repoDir, file.name).replace(root, "")
      warnings.push(`${relativePath}: unregistered secret-like identifier(s): ${[...new Set(matches.map((m) => m[0]))].join(", ")}`)
    }
  }
}

if (warnings.length > 0) {
  console.error(warnings.join("\n"))
}

if (violations.length > 0) {
  console.error(violations.join("\n"))
  process.exit(1)
}

console.log(
  `Encrypted-column invariant OK: ${(registry.columns ?? []).length} registered columns, ${warnings.length} heuristic warning(s)`
)