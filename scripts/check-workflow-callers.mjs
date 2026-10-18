import { readFile, readdir } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

// Every service that opens a workflow-service gRPC client from an HTTP handler must
//   1. bind the BFF identity headers to the outgoing gRPC metadata by wrapping its router in
//      ardametadata.HTTPMiddleware, otherwise workflow-service sees an empty tenant and rejects
//      every CreateCase/SubmitCase with "verified tenant scope is required" (gateway: 502);
//   2. be listed as an allowed source of workflow-service's gRPC ServiceAuth interceptor,
//      otherwise the call is refused with PermissionDenied.

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const appsRoot = join(root, "apps")
const workflowMainPath = join(appsRoot, "workflow-service/cmd/workflow-service/main.go")

async function readOptional(path) {
  try {
    return await readFile(path, "utf8")
  } catch (error) {
    if (error.code === "ENOENT") return ""
    throw error
  }
}

async function* goFiles(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) yield* goFiles(path)
    else if (entry.isFile() && entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) yield path
  }
}

const workflowMain = await readFile(workflowMainPath, "utf8")
const allowList = workflowMain.match(/UnaryServerServiceAuth\(\s*serviceSecret,\s*"workflow-service",\s*map\[string\]struct\{\}\{([\s\S]*?)\}\),/)
if (!allowList) throw new Error("workflow-service main.go: cannot find the gRPC ServiceAuth allow-list")
const allowedSources = new Set([...allowList[1].matchAll(/"([a-z-]+)"/g)].map((match) => match[1]))

const dialPattern = /workflowclient\.Dial\(/
const violations = []
const callers = []
for (const app of await readdir(appsRoot, { withFileTypes: true })) {
  if (!app.isDirectory() || app.name === "workflow-service") continue
  let dials = false
  for await (const file of goFiles(join(appsRoot, app.name))) {
    if (dialPattern.test(await readFile(file, "utf8"))) {
      dials = true
      break
    }
  }
  if (!dials) continue
  callers.push(app.name)

  const router = await readOptional(join(appsRoot, app.name, "internal/transport/http/router.go"))
  if (!router.includes("ardametadata.HTTPMiddleware(")) {
    violations.push(`${app.name}: dials workflow-service but its HTTP router is not wrapped in ardametadata.HTTPMiddleware`)
  }
  if (!allowedSources.has(app.name)) {
    violations.push(`${app.name}: dials workflow-service but is missing from the ServiceAuth allow-list in apps/workflow-service/cmd/workflow-service/main.go`)
  }
}

if (callers.length === 0) throw new Error("no workflow-service caller found; the check pattern is stale")
if (violations.length > 0) {
  console.error(violations.join("\n"))
  process.exit(1)
}
console.log(`workflow caller scope check passed (${callers.sort().join(", ")})`)
