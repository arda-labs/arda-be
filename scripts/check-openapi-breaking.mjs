// ADR-005: the committed OpenAPI documents are the contract the frontend and
// docs-site consume, so a change that breaks an existing client must be
// deliberate. This gate compares every contracts/openapi/*-v1.json with the
// copy on the base branch (origin/main) and fails on:
//
//   - a removed path or operation;
//   - a field removed from a response schema, or a response field whose type
//     gained a value (for example string -> string|null);
//   - a request field or parameter that is now required, a request field
//     whose type lost a value, or a request body that is now required;
//   - an enum that lost values, on either side;
//   - a changed success status code, or a lost request/response content type.
//
// $refs are resolved inside each document, so renaming a component schema is
// not a change: only the wire shape is compared. A document that does not
// exist on the base branch is new and skipped.
//
// A deliberate break is recorded in scripts/check-openapi-breaking-allowlist.json
// as {document, change, reason, expires}; the change is the exact message
// this gate prints (without the document prefix) and the entry stops
// suppressing it after `expires` (YYYY-MM-DD). The allowlist lives outside
// contracts/openapi because docs-site publishes every .json there.
//
//   node scripts/check-openapi-breaking.mjs [--base origin/main]
//
// CI fetches origin/main first; locally the remote-tracking branch is used.
import { execFileSync } from "node:child_process"
import { readdir, readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const METHODS = ["get", "post", "put", "patch", "delete"]

function resolveRef(schema, doc) {
  let seen = 0
  while (schema && typeof schema.$ref === "string" && seen++ < 20) {
    const name = schema.$ref.replace(/^#\/components\/schemas\//, "")
    schema = doc.components?.schemas?.[name]
  }
  return schema ?? {}
}

// normalize flattens a schema into {types, enum, properties, required, items,
// additionalProperties}. A nullable anyOf [X, {type: null}] becomes X plus
// the null type; a const is a one-value enum.
function normalize(schema, doc) {
  schema = resolveRef(schema, doc)
  const out = { types: new Set(), enum: null, properties: null, required: new Set(), items: null, additionalProperties: null }
  if (Array.isArray(schema.anyOf) || Array.isArray(schema.oneOf)) {
    const members = (schema.anyOf ?? schema.oneOf).map((m) => resolveRef(m, doc))
    const nulls = members.filter((m) => m.type === "null")
    const rest = members.filter((m) => m.type !== "null")
    if (rest.length === 1) {
      const inner = normalize(rest[0], doc)
      if (nulls.length) inner.types.add("null")
      return inner
    }
  }
  for (const t of Array.isArray(schema.type) ? schema.type : schema.type ? [schema.type] : []) out.types.add(t)
  if (Array.isArray(schema.enum)) out.enum = schema.enum.map(String)
  else if ("const" in schema) out.enum = [String(schema.const)]
  if (schema.properties && typeof schema.properties === "object") out.properties = schema.properties
  for (const name of Array.isArray(schema.required) ? schema.required : []) out.required.add(name)
  if (schema.items && typeof schema.items === "object") out.items = schema.items
  if (schema.additionalProperties && typeof schema.additionalProperties === "object") out.additionalProperties = schema.additionalProperties
  return out
}

const fmt = (set) => [...set].sort().join("|")
const child = (label, name) => (label ? `${label}.${name}` : name)

// compareSchema appends the breaking differences between old and new. In a
// response the new shape must stay a subset of what clients already handle;
// in a request the new shape must keep accepting what clients already send.
function compareSchema(oldSchema, newSchema, oldDoc, newDoc, direction, label, what, out, seen) {
  const key = `${oldSchema?.$ref ?? ""}|${newSchema?.$ref ?? ""}`
  if (oldSchema?.$ref && newSchema?.$ref) {
    if (seen.has(key)) return
    seen.add(key)
  }
  const o = normalize(oldSchema, oldDoc)
  const n = normalize(newSchema, newDoc)
  const name = label ? `${what} ${label}` : what
  if (o.types.size && n.types.size) {
    const lost = direction === "response" ? [...n.types].filter((t) => !o.types.has(t)) : [...o.types].filter((t) => !n.types.has(t))
    if (lost.length) out.push(`${name} type ${fmt(o.types)} changed to ${fmt(n.types)}`)
  }
  if (o.enum) {
    if (n.enum) {
      const lost = o.enum.filter((v) => !n.enum.includes(v))
      if (lost.length) out.push(`${name} enum lost values ${lost.join(", ")}`)
    }
  } else if (n.enum && direction === "request") {
    out.push(`${name} is now restricted to ${n.enum.join(", ")}`)
  }
  if (o.properties || n.properties) {
    const oldProps = o.properties ?? {}
    const newProps = n.properties ?? {}
    for (const prop of Object.keys(oldProps).sort()) {
      if (!(prop in newProps)) {
        if (direction === "response") out.push(`${what} ${child(label, prop)} removed`)
        continue
      }
      compareSchema(oldProps[prop], newProps[prop], oldDoc, newDoc, direction, child(label, prop), what, out, seen)
    }
    if (direction === "request") {
      for (const prop of [...n.required].sort()) {
        if (!o.required.has(prop)) out.push(`${what} ${child(label, prop)} is now required`)
      }
    }
  }
  if (o.items && n.items) compareSchema(o.items, n.items, oldDoc, newDoc, direction, `${label}[]`, what, out, seen)
  if (o.additionalProperties && n.additionalProperties) {
    compareSchema(o.additionalProperties, n.additionalProperties, oldDoc, newDoc, direction, `${label}.*`, what, out, seen)
  }
}

function compareOperation(route, oldOp, newOp, oldDoc, newDoc, out) {
  const seen = new Set()
  const say = (text) => out.push(`${route}: ${text}`)
  const local = []
  const paramKey = (p) => `${p.in}:${p.name}`
  const oldParams = new Map((oldOp.parameters ?? []).map((p) => [paramKey(p), p]))
  for (const param of newOp.parameters ?? []) {
    const before = oldParams.get(paramKey(param))
    if (param.required && !(before?.required)) say(`parameter ${param.name} (${param.in}) is now required`)
    if (before?.schema && param.schema) {
      compareSchema(before.schema, param.schema, oldDoc, newDoc, "request", `${param.name} (${param.in})`, "parameter", local, seen)
    }
  }
  if (!oldOp.requestBody && newOp.requestBody?.required) say("request body is now required")
  if (oldOp.requestBody && newOp.requestBody) {
    for (const [type, media] of Object.entries(oldOp.requestBody.content ?? {})) {
      const after = newOp.requestBody.content?.[type]
      if (!after) {
        say(`request body content type ${type} removed`)
        continue
      }
      if (media.schema && after.schema) compareSchema(media.schema, after.schema, oldDoc, newDoc, "request", "", "request field", local, seen)
    }
  }
  const success = (op) => Object.keys(op.responses ?? {}).filter((s) => s.startsWith("2"))
  const newSuccess = success(newOp)
  for (const status of success(oldOp)) {
    if (!newSuccess.includes(status)) {
      say(`success status ${status} changed to ${newSuccess.join("/") || "none"}`)
      continue
    }
    const before = oldOp.responses[status]
    const after = newOp.responses[status]
    for (const [type, media] of Object.entries(before.content ?? {})) {
      const next = after.content?.[type]
      if (!next) {
        say(`response ${status} lost content type ${type}`)
        continue
      }
      if (media.schema && next.schema) compareSchema(media.schema, next.schema, oldDoc, newDoc, "response", "", `response ${status} field`, local, seen)
    }
  }
  for (const text of local) say(text)
}

// compareDocuments returns the breaking changes from oldDoc to newDoc.
export function compareDocuments(oldDoc, newDoc) {
  const out = []
  for (const [path, item] of Object.entries(oldDoc.paths ?? {})) {
    const after = newDoc.paths?.[path]
    if (!after) {
      out.push(`path ${path} removed`)
      continue
    }
    for (const method of METHODS) {
      if (!item[method]) continue
      const route = `${method.toUpperCase()} ${path}`
      if (!after[method]) {
        out.push(`operation ${route} removed`)
        continue
      }
      compareOperation(route, item[method], after[method], oldDoc, newDoc, out)
    }
  }
  return out
}

// applyAllowlist drops the changes recorded for `document` whose entry has
// not expired on `today` (YYYY-MM-DD) and reports the entries it used.
export function applyAllowlist(document, changes, entries, today) {
  const active = entries.filter((e) => e.document === document && typeof e.change === "string" && typeof e.reason === "string" && typeof e.expires === "string" && e.expires >= today)
  const used = new Set()
  const failures = []
  for (const change of changes) {
    const entry = active.find((e) => e.change === change)
    if (entry) used.add(entry)
    else failures.push(change)
  }
  return { failures, used: [...used] }
}

function gitShow(ref, path) {
  try {
    return execFileSync("git", ["show", `${ref}:${path}`], { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"], maxBuffer: 64 * 1024 * 1024 })
  } catch {
    return null
  }
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (isMain) {
  const baseIndex = process.argv.indexOf("--base")
  const base = baseIndex > 0 ? process.argv[baseIndex + 1] : "origin/main"
  try {
    execFileSync("git", ["rev-parse", "--verify", "--quiet", `${base}^{commit}`], { cwd: root, stdio: "ignore" })
  } catch {
    console.error(`${base} is not available; run git fetch origin main first`)
    process.exit(1)
  }
  const allowlistPath = join(root, "scripts/check-openapi-breaking-allowlist.json")
  const entries = JSON.parse(await readFile(allowlistPath, "utf8"))
  if (!Array.isArray(entries)) throw new Error(`${allowlistPath} must be a JSON array`)
  const today = new Date().toISOString().slice(0, 10)
  const dir = join(root, "contracts/openapi")
  const files = (await readdir(dir)).filter((name) => /-v\d+\.json$/.test(name)).sort()
  const failures = []
  const usedEntries = new Set()
  let compared = 0
  for (const file of files) {
    const path = `contracts/openapi/${file}`
    const before = gitShow(base, path)
    if (before === null) {
      console.log(`${file}: not on ${base}, skipped (new document)`)
      continue
    }
    const after = await readFile(join(dir, file), "utf8")
    compared += 1
    if (before.replaceAll("\r\n", "\n") === after.replaceAll("\r\n", "\n")) continue
    const changes = compareDocuments(JSON.parse(before), JSON.parse(after))
    const { failures: open, used } = applyAllowlist(file, changes, entries, today)
    for (const entry of used) usedEntries.add(entry)
    for (const change of open) failures.push(`${file}: ${change}`)
  }
  for (const entry of entries) {
    if (entry.expires < today) console.warn(`allowlist entry expired on ${entry.expires}: ${entry.document}: ${entry.change}`)
    else if (!usedEntries.has(entry)) console.warn(`allowlist entry matches nothing against ${base}: ${entry.document}: ${entry.change}`)
  }
  if (failures.length) {
    console.error(failures.join("\n"))
    console.error(
      "\nThese changes break existing clients of the OpenAPI contract. Keep the old shape, or record each change " +
        "with a reason and expiry in scripts/check-openapi-breaking-allowlist.json.",
    )
    process.exit(1)
  }
  console.log(`OpenAPI breaking-change check OK: ${compared} document(s) compared with ${base}, ${usedEntries.size} allowlisted change(s)`)
}
