import { readdir, readFile } from "node:fs/promises"

const contractsURL = new URL("../contracts/openapi/", import.meta.url)
const files = (await readdir(contractsURL)).filter((name) => name.endsWith(".json")).sort()
if (files.length === 0) throw new Error("no OpenAPI documents found")

let totalOperations = 0
for (const file of files) {
  const path = new URL(file, contractsURL)
  const document = JSON.parse(await readFile(path, "utf8"))

  if (document.openapi !== "3.1.0") throw new Error(`${file}: OpenAPI 3.1.0 is required`)
  if (!document.paths || Object.keys(document.paths).length === 0) {
    throw new Error(`${file}: OpenAPI document has no paths`)
  }

  const operations = []
  for (const [route, item] of Object.entries(document.paths)) {
    for (const [method, operation] of Object.entries(item)) {
      if (!["get", "post", "put", "patch", "delete"].includes(method)) continue
      if (!operation.operationId) throw new Error(`${file}: ${method.toUpperCase()} ${route} has no operationId`)
      operations.push(operation)
    }
  }

  const schemas = document.components?.schemas ?? {}
  for (const required of ["ResponseMeta", "Problem"]) {
    if (!schemas[required]) throw new Error(`${file}: missing canonical schema ${required}`)
  }
  if (document.components?.securitySchemes?.ardaSession?.["x-browser-credentials"] !== "include") {
    throw new Error(`${file}: browser API security scheme must require credentials: include`)
  }
  for (const operation of operations) {
    const hasSuccessResponse = Object.keys(operation.responses ?? {}).some((status) => {
      const first = status[0]
      return first === "2" || first === "3"
    })
    if (!hasSuccessResponse) {
      throw new Error(`${file}: ${operation.operationId} has no 2xx/3xx success response`)
    }
  }
  totalOperations += operations.length
}
console.log(`OpenAPI contract OK: ${files.length} documents, ${totalOperations} operations`)
