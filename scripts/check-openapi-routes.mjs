// ADR-005: a service whose public REST surface is generated into
// contracts/openapi must document every public route it serves, and nothing
// it does not serve. This gate reads the service's router.go, collects each
// /api/ route with its HTTP methods (from a "METHOD /path" pattern, from the
// http.Method* cases of a method-switching closure, or from a method("X", …)
// wrapper) and compares them with the operations in the generated document.
//
// Add a service to `covered` when its document is generated from Go
// (ardaopenapi). Services not listed keep their hand-written pilot documents.
import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))

const covered = {
  "deposit-service": "contracts/openapi/deposit-v1.json",
}

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"]

export function routerRoutes(source) {
  const routes = new Set()
  const problems = []
  const pattern = /mux\.Handle(?:Func)?\(\s*"([^"]+)"/g
  const matches = [...source.matchAll(pattern)]
  matches.forEach((match, index) => {
    const spec = match[1]
    const [first, ...rest] = spec.split(" ")
    const hasMethod = METHODS.includes(first) && rest.length === 1
    const path = hasMethod ? rest[0] : spec
    if (!path.startsWith("/api/")) return
    if (hasMethod) {
      routes.add(`${first} ${path}`)
      return
    }
    const end = index + 1 < matches.length ? matches[index + 1].index : source.length
    const body = source.slice(match.index, end)
    const methods = new Set()
    for (const [, name] of body.matchAll(/http\.Method(Get|Post|Put|Patch|Delete)\b/g)) methods.add(name.toUpperCase())
    for (const [, name] of body.matchAll(/method\(\s*"([A-Z]+)"/g)) methods.add(name)
    if (methods.size === 0) problems.push(`cannot tell which methods ${path} serves`)
    for (const method of methods) routes.add(`${method} ${path}`)
  })
  return { routes, problems }
}

export function documentRoutes(document) {
  const routes = new Set()
  for (const [path, item] of Object.entries(document.paths ?? {})) {
    for (const method of Object.keys(item)) {
      if (METHODS.includes(method.toUpperCase())) routes.add(`${method.toUpperCase()} ${path}`)
    }
  }
  return routes
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (isMain) {
  const failures = []
  let total = 0
  for (const [service, documentPath] of Object.entries(covered)) {
    const router = await readFile(join(root, "apps", service, "internal/transport/http/router.go"), "utf8")
    const document = JSON.parse(await readFile(join(root, documentPath), "utf8"))
    const { routes, problems } = routerRoutes(router)
    const documented = documentRoutes(document)
    for (const problem of problems) failures.push(`${service}: ${problem}`)
    for (const route of [...routes].sort()) {
      if (!documented.has(route)) failures.push(`${service}: ${route} is served but missing from ${documentPath}`)
    }
    for (const route of [...documented].sort()) {
      if (!routes.has(route)) failures.push(`${service}: ${route} is documented in ${documentPath} but not served`)
    }
    total += routes.size
  }
  if (failures.length) {
    console.error(failures.join("\n"))
    console.error(
      "\nDeclare the operation in internal/transport/http/openapi.go and regenerate with " +
        "ARDA_UPDATE_OPENAPI=1 go test ./internal/transport/http -run TestOpenAPIDocumentIsCommitted.",
    )
    process.exit(1)
  }
  console.log(`OpenAPI route coverage OK: ${Object.keys(covered).length} service(s), ${total} public routes documented`)
}
