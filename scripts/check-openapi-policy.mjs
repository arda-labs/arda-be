// ADR-005: every browser request goes through auth-gateway, whose
// configs/policy.yaml decides which routes exist and what they require. For a
// service whose document is generated from Go (the `covered` list of
// check-openapi-routes.mjs) this gate keeps policy.yaml and the document in
// step, in both directions:
//
//   - every policy route under the service prefix (the "/api/<service>/" the
//     document's paths share) must match at least one documented operation,
//     otherwise it guards a route the service does not serve;
//   - every documented operation must be covered by at least one policy
//     route, otherwise the gateway never lets it through.
//
// Paths are matched the way auth-gateway/internal/policy matches them: a
// trailing "/**" or "/*" is a prefix (the prefix itself or anything below
// it), anything else is exact, trailing slashes are ignored, and an empty
// methods list means every method. A {param} in a documented path stands
// for one non-empty segment; a path parameter with an enum is expanded to
// its values first, so a policy route may name a single catalog. OPTIONS and
// HEAD in a policy route are ignored: documents only list the five JSON
// methods.
//
// Not in scope: policy routes outside /api/ (auth, health) and the
// /internal/ service-to-service routes, which no policy points at and no
// document lists. Server-sent event routes would be excluded the same way,
// but none of the covered services serves one.
//
// A real mismatch is an authorization question, not something this gate
// fixes: record it in scripts/check-openapi-policy-baseline.json with a note
// and it is reported as known instead of failing. A baseline entry that no
// longer matches anything fails so the file stays current.
//
//   node scripts/check-openapi-policy.mjs
import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { covered } from "./check-openapi-routes.mjs"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"]

// parsePolicyRoutes reads the `routes:` list of policy.yaml (id, path,
// methods, auth, risk). The file is flat enough that a line parser is exact;
// a route without an id or path is reported.
export function parsePolicyRoutes(source) {
  const routes = []
  let current = null
  for (const raw of source.split(/\r?\n/)) {
    const line = raw.replace(/\s+#.*$/, "")
    const start = line.match(/^\s*-\s+id:\s*(\S+)\s*$/)
    if (start) {
      current = { id: start[1], path: "", methods: [], auth: null }
      routes.push(current)
      continue
    }
    if (!current) continue
    const field = line.match(/^\s+(path|methods|auth|risk):\s*(.*?)\s*$/)
    if (!field) continue
    const [, key, value] = field
    if (key === "path") current.path = value
    else if (key === "auth") current.auth = value === "true"
    else if (key === "methods") {
      current.methods = value
        .replace(/^\[|\]$/g, "")
        .split(",")
        .map((m) => m.trim().toUpperCase())
        .filter(Boolean)
    }
  }
  return routes
}

// expandOperations lists {method, path, segments, operationId} per documented
// operation, with enum path parameters expanded to each value.
export function expandOperations(document) {
  const out = []
  for (const [path, item] of Object.entries(document.paths ?? {})) {
    for (const method of Object.keys(item)) {
      if (!METHODS.includes(method.toUpperCase())) continue
      const op = item[method]
      const enums = new Map()
      for (const param of op.parameters ?? []) {
        if (param.in === "path" && Array.isArray(param.schema?.enum)) enums.set(param.name, param.schema.enum.map(String))
      }
      let paths = [path]
      for (const [name, values] of enums) {
        paths = paths.flatMap((p) => values.map((v) => p.replaceAll(`{${name}}`, v)))
      }
      for (const concrete of paths) out.push({ method: method.toUpperCase(), path: concrete, operationId: op.operationId, template: path })
    }
  }
  return out
}

const segments = (path) => path.replace(/\/+$/, "").split("/").filter(Boolean)
const isParam = (segment) => /^\{[^}]+\}$/.test(segment)

// policyPrefix returns the prefix segments of a pattern and whether it is a
// prefix pattern, mirroring policy.pathMatches.
function parsePattern(pattern) {
  const trimmed = pattern.replace(/\/+$/, "")
  if (trimmed.endsWith("/**") || trimmed.endsWith("/*")) {
    return { prefix: true, segments: segments(trimmed.replace(/\/\*\*?$/, "")) }
  }
  return { prefix: false, segments: segments(trimmed) }
}

// overlaps: some concrete request path satisfies both the policy pattern and
// the documented path (a {param} segment matches any policy literal).
export function overlaps(pattern, documentedPath) {
  const p = parsePattern(pattern)
  const d = segments(documentedPath)
  if (p.prefix ? d.length < p.segments.length : d.length !== p.segments.length) return false
  return p.segments.every((seg, i) => seg === d[i] || isParam(d[i]))
}

// covers: every concrete request to the documented path matches the policy
// pattern. A literal policy segment cannot cover a {param} segment.
export function covers(pattern, documentedPath) {
  const p = parsePattern(pattern)
  const d = segments(documentedPath)
  if (p.prefix ? d.length < p.segments.length : d.length !== p.segments.length) return false
  return p.segments.every((seg, i) => seg === d[i] && !isParam(d[i]))
}

const methodAllowed = (route, method) => route.methods.length === 0 || route.methods.includes(method)

// servicePrefix returns the "/api/<name>/" every path of the document shares.
export function servicePrefix(document) {
  const prefixes = new Set(Object.keys(document.paths ?? {}).map((p) => "/" + segments(p).slice(0, 2).join("/") + "/"))
  if (prefixes.size !== 1) throw new Error(`document paths do not share one /api/<service>/ prefix: ${[...prefixes].join(", ")}`)
  return [...prefixes][0]
}

// comparePolicy returns the mismatches between the policy routes under the
// service prefix and the documented operations.
export function comparePolicy(service, routes, document) {
  const prefix = servicePrefix(document)
  const operations = expandOperations(document)
  const underPrefix = routes.filter((route) => {
    const p = parsePattern(route.path)
    const pre = segments(prefix)
    return p.segments.slice(0, pre.length).join("/") === pre.join("/") || (p.prefix && p.segments.length < pre.length && pre.slice(0, p.segments.length).join("/") === p.segments.join("/"))
  })
  const failures = []
  for (const route of underPrefix) {
    const methods = route.methods.filter((m) => METHODS.includes(m))
    if (route.methods.length > 0 && methods.length === 0) continue
    const matched = operations.some((op) => (methods.length === 0 || methods.includes(op.method)) && overlaps(route.path, op.path))
    if (!matched) failures.push(`${service}: policy route ${route.id} (${route.methods.join(",") || "ALL"} ${route.path}) matches no documented operation`)
  }
  for (const op of operations) {
    const coveredBy = underPrefix.some((route) => methodAllowed(route, op.method) && covers(route.path, op.path))
    if (!coveredBy) failures.push(`${service}: ${op.method} ${op.path} (${op.operationId}) is covered by no policy route, the gateway never forwards it`)
  }
  return failures
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (isMain) {
  const routes = parsePolicyRoutes(await readFile(join(root, "apps/auth-gateway/configs/policy.yaml"), "utf8"))
  if (routes.length === 0) throw new Error("no routes parsed from policy.yaml")
  for (const route of routes) {
    if (!route.path.startsWith("/")) throw new Error(`policy route ${route.id} has no path`)
  }
  const baseline = JSON.parse(await readFile(join(root, "scripts/check-openapi-policy-baseline.json"), "utf8"))
  const known = new Map(baseline.map((entry) => [entry.mismatch, entry]))
  const failures = []
  const seenKnown = new Set()
  let checked = 0
  for (const [service, documentPath] of Object.entries(covered)) {
    const document = JSON.parse(await readFile(join(root, documentPath), "utf8"))
    for (const mismatch of comparePolicy(service, routes, document)) {
      if (known.has(mismatch)) {
        seenKnown.add(mismatch)
        console.log(`known: ${mismatch} (${known.get(mismatch).note})`)
      } else failures.push(mismatch)
    }
    checked += 1
  }
  for (const mismatch of known.keys()) {
    if (!seenKnown.has(mismatch)) failures.push(`baseline entry no longer matches anything, remove it: ${mismatch}`)
  }
  if (failures.length) {
    console.error(failures.join("\n"))
    console.error("\npolicy.yaml and the generated OpenAPI documents disagree. Changing policy.yaml changes authorization; record a real mismatch in scripts/check-openapi-policy-baseline.json with a note for review instead of editing the policy here.")
    process.exit(1)
  }
  console.log(`OpenAPI policy check OK: ${checked} service(s), ${routes.length} policy routes, ${seenKnown.size} known mismatch(es)`)
}
