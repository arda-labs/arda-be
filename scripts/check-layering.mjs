import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const checks = [
  {
    file: "apps/media-service/internal/handler/media_handler.go",
    required: ["internal/domain", "internal/service"],
    forbidden: ["internal/repository"],
  },
  {
    file: "apps/media-service/internal/service/media_service.go",
    required: ["internal/domain", "internal/repository", "internal/storage"],
    forbidden: [],
  },
]
const errors = []

for (const check of checks) {
  const source = await readFile(join(root, check.file), "utf8")
  for (const importPath of check.required) {
    if (!source.includes(importPath)) errors.push(`${check.file}: missing required layer ${importPath}`)
  }
  for (const importPath of check.forbidden) {
    if (source.includes(importPath)) errors.push(`${check.file}: forbidden layer dependency ${importPath}`)
  }
}

if (errors.length) {
  console.error(errors.join("\n"))
  process.exit(1)
}

console.log(`Layering invariant OK: ${checks.length} reference files checked`)
