// ADR-005: a service whose public REST surface is generated into
// contracts/openapi must document every public route it serves, and nothing
// it does not serve. This gate reads the service's router.go, collects each
// /api/ route with its HTTP methods (from a "METHOD /path" pattern, from the
// http.Method* cases of a method-switching closure or handler method, or from
// a method("X", ...) wrapper) and compares them with the operations in the
// generated document. A pattern built in a loop, such as
// `base := "/api/loan/adjustments/" + kind`, is read as the template
// /api/loan/adjustments/{kind}.
//
// A pattern ending in "/" is a Go mux subtree ("/api/workflow/cases/"): the
// handler dispatches on the remaining path itself, so the gate cannot list its
// sub-paths. It takes the methods the handler (or the closure and every
// handler method it calls) switches on, and requires at least one documented
// operation under the prefix for each of them; an operation under the prefix
// with one of those methods counts as served.
//
// Add a service to `covered` when its document is generated from Go
// (ardaopenapi). Services not listed keep their hand-written pilot documents.
//
//   node scripts/check-openapi-routes.mjs           check
//   node scripts/check-openapi-routes.mjs --print   list the routes each router serves
import { readdir, readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))

export const covered = {
  "deposit-service": "contracts/openapi/deposit-v1.json",
  "loan-service": "contracts/openapi/loan-v1.json",
  "workflow-service": "contracts/openapi/workflow-v1.json",
  "mdm-service": "contracts/openapi/mdm-v1.json",
}

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"]

// routerRoutes returns the "METHOD /path" routes a router.go serves and the
// subtrees (prefix -> methods) it delegates to a dispatching handler.
// handlerSources maps a handler method name to its source, so a handler that
// switches on r.Method itself can be read like a closure.
export function routerRoutes(source, handlerSources = new Map()) {
  const routes = new Set()
  const subtrees = new Map()
  const problems = []
  // Templates for identifiers assigned as `x := "/literal/" + loopVar`.
  const templates = new Map()
  for (const [, name, prefix, variable] of source.matchAll(/(\w+)\s*:=\s*"([^"]+)"\s*\+\s*(\w+)/g)) {
    templates.set(name, `${prefix}{${variable}}`)
  }
  const pattern = /mux\.Handle(?:Func)?\(\s*(?:"([^"]+)"|(\w+)(?:\s*\+\s*"([^"]*)")?)\s*,/g
  const matches = [...source.matchAll(pattern)]
  matches.forEach((match, index) => {
    let spec = match[1]
    if (spec === undefined) {
      const template = templates.get(match[2])
      if (!template) {
        problems.push(`cannot resolve route pattern built from ${match[2]}`)
        return
      }
      spec = template + (match[3] ?? "")
    }
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
    const collect = (text) => {
      for (const [, name] of text.matchAll(/http\.Method(Get|Post|Put|Patch|Delete)\b/g)) methods.add(name.toUpperCase())
      for (const [, name] of text.matchAll(/method\(\s*"([A-Z]+)"/g)) methods.add(name)
    }
    collect(body)
    // The handler passed directly, or every handler method a closure calls.
    const direct = body.match(/,\s*\w+\.(\w+)\s*\)/)
    const called = [...body.matchAll(/\w+\.(\w+)\(w, r\)/g)].map((m) => m[1])
    for (const name of [direct?.[1], ...called]) {
      if (name && handlerSources.has(name)) collect(handlerSources.get(name))
    }
    if (methods.size === 0) problems.push(`cannot tell which methods ${path} serves`)
    if (path.endsWith("/") && !path.includes("{")) {
      subtrees.set(path, new Set([...(subtrees.get(path) ?? []), ...methods]))
      return
    }
    for (const method of methods) routes.add(`${method} ${path}`)
  })
  return { routes, subtrees, problems }
}

// loadHandlerSources indexes the methods declared in a handler directory.
export async function loadHandlerSources(dir) {
  const sources = new Map()
  let names = []
  try {
    names = await readdir(dir)
  } catch {
    return sources
  }
  for (const name of names) {
    if (!name.endsWith(".go") || name.endsWith("_test.go")) continue
    const text = await readFile(join(dir, name), "utf8")
    const funcs = [...text.matchAll(/^func \(\w+ \*?\w+\) (\w+)\(/gm)]
    funcs.forEach((fn, i) => {
      const stop = i + 1 < funcs.length ? funcs[i + 1].index : text.length
      sources.set(fn[1], (sources.get(fn[1]) ?? "") + text.slice(fn.index, stop))
    })
  }
  return sources
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

// compareRoutes lists the differences between what a router serves and what
// its document declares. Messages name the service and document.
export function compareRoutes(service, documentPath, { routes, subtrees, problems }, documented) {
  const failures = problems.map((problem) => `${service}: ${problem}`)
  const underSubtree = (method, path) => {
    for (const [prefix, methods] of subtrees) {
      if (path.startsWith(prefix) && path.length > prefix.length && methods.has(method)) return true
    }
    return false
  }
  for (const route of [...routes].sort()) {
    if (!documented.has(route)) failures.push(`${service}: ${route} is served but missing from ${documentPath}`)
  }
  for (const [prefix, methods] of [...subtrees].sort()) {
    for (const method of [...methods].sort()) {
      const some = [...documented].some((route) => {
        const [m, path] = route.split(" ")
        return m === method && path.startsWith(prefix) && path.length > prefix.length
      })
      if (!some) failures.push(`${service}: ${method} ${prefix}* is served but no operation under it is documented in ${documentPath}`)
    }
  }
  for (const route of [...documented].sort()) {
    const [method, path] = route.split(" ")
    if (!routes.has(route) && !underSubtree(method, path)) {
      failures.push(`${service}: ${route} is documented in ${documentPath} but not served`)
    }
  }
  return failures
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (isMain) {
  const failures = []
  let total = 0
  for (const [service, documentPath] of Object.entries(covered)) {
    const router = await readFile(join(root, "apps", service, "internal/transport/http/router.go"), "utf8")
    const handlers = await loadHandlerSources(join(root, "apps", service, "internal/handler"))
    const served = routerRoutes(router, handlers)
    if (process.argv.includes("--print")) {
      console.log(`${service}:\n  ${[...served.routes].sort().join("\n  ")}`)
      for (const [prefix, methods] of [...served.subtrees].sort()) console.log(`  ${[...methods].sort().join("|")} ${prefix}*`)
      for (const problem of served.problems) console.log(`  ! ${problem}`)
      continue
    }
    const document = JSON.parse(await readFile(join(root, documentPath), "utf8"))
    const documented = documentRoutes(document)
    failures.push(...compareRoutes(service, documentPath, served, documented))
    total += documented.size
  }
  if (process.argv.includes("--print")) process.exit(0)
  if (failures.length) {
    console.error(failures.join("\n"))
    console.error(
      "\nDeclare the operation in internal/transport/http/openapi.go and regenerate with " +
        "ARDA_UPDATE_OPENAPI=1 go test ./internal/transport/http -run TestOpenAPIDocumentIsCommitted.",
    )
    process.exit(1)
  }
  console.log(`OpenAPI route coverage OK: ${Object.keys(covered).length} service(s), ${total} public operations documented`)
}
