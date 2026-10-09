import { readFile, readdir } from "node:fs/promises"
import { join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const baselinePath = join(root, "scripts/check-layering-baseline.json")
const baseline = JSON.parse(await readFile(baselinePath, "utf8"))

if (!Array.isArray(baseline) || baseline.some((entry) => typeof entry !== "string")) {
  throw new Error("Layering baseline must be a JSON array of repository-relative file paths")
}
if (new Set(baseline).size !== baseline.length) {
  throw new Error("Layering baseline contains duplicate file paths")
}

function unquoteImportPath(literal) {
  if (literal.startsWith("`")) return literal.slice(1, -1)
  return JSON.parse(literal)
}

function importPaths(source) {
  const paths = []
  const blockPattern = /^\s*import\s*\(([\s\S]*?)^\s*\)/gm
  const specPattern = /^\s*(?:[\w.]+\s+)?("(?:\\.|[^"\\])*"|`[^`]*`)\s*(?:\/\/.*)?$/gm

  for (const block of source.matchAll(blockPattern)) {
    for (const spec of block[1].matchAll(specPattern)) paths.push(unquoteImportPath(spec[1]))
  }

  const singlePattern = /^\s*import\s+(?:[\w.]+\s+)?("(?:\\.|[^"\\])*"|`[^`]*`)\s*(?:\/\/.*)?$/gm
  for (const single of source.matchAll(singlePattern)) paths.push(unquoteImportPath(single[1]))
  return paths
}

async function* goFiles(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) yield* goFiles(path)
    else if (entry.isFile() && entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) yield path
  }
}

const violations = []
let scannedFiles = 0
for (const app of await readdir(join(root, "apps"), { withFileTypes: true })) {
  if (!app.isDirectory()) continue
  const handlerRoot = join(root, "apps", app.name, "internal", "handler")
  let files
  try {
    files = goFiles(handlerRoot)
    await readdir(handlerRoot)
  } catch (error) {
    if (error.code === "ENOENT") continue
    throw error
  }

  for await (const file of files) {
    scannedFiles += 1
    const importsRepository = importPaths(await readFile(file, "utf8")).some((path) =>
      path.endsWith("/internal/repository"),
    )
    if (importsRepository) violations.push(relative(root, file).replaceAll("\\", "/"))
  }
}

violations.sort()
const baselineSet = new Set(baseline)
const newViolations = violations.filter((file) => !baselineSet.has(file))
const resolvedBaseline = baseline.filter((file) => !violations.includes(file))

if (newViolations.length > 0) {
  console.error(`Layering invariant failed: ${newViolations.length} new handler-to-repository import(s)`)
  for (const file of newViolations) console.error(`  ${file}`)
  process.exit(1)
}

console.log(
  `Layering invariant OK: scanned ${scannedFiles} production handlers; ${violations.length} known import(s) in baseline; ${resolvedBaseline.length} baseline item(s) resolved`,
)
