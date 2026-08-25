import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const contractPath = resolve(root, "contracts/observability/arda-observability-v1.json")
const contract = JSON.parse(await readFile(contractPath, "utf8"))
const source = await readFile(resolve(root, "libs/go/arda-http/observability.go"), "utf8")
const requiredMetrics = [
  "arda_http_requests_total",
  "arda_http_responses_total",
  "arda_http_request_duration_seconds",
]
const errors = []

if (contract.version !== "1.0.0") errors.push("observability contract must be version 1.0.0")
if (contract.status !== "proposed") errors.push("SLO targets require explicit approval before becoming active")
for (const metric of requiredMetrics) {
  if (!contract.metrics.some((entry) => entry.name === metric)) errors.push(`contract missing metric ${metric}`)
  if (!source.includes(metric)) errors.push(`arda-http implementation missing metric ${metric}`)
}
for (const header of ["X-Request-Id", "traceparent", "X-Trace-Id"]) {
  if (!JSON.stringify(contract.correlation).includes(header)) errors.push(`contract missing correlation header ${header}`)
}
if (!Array.isArray(contract.runtime_requirements) || contract.runtime_requirements.length < 4) {
  errors.push("runtime exporter/dashboard requirements are incomplete")
}

if (errors.length) {
  console.error(errors.join("\n"))
  process.exit(1)
}
console.log(`Observability contract OK: ${requiredMetrics.length} metrics, ${contract.slo_classes.length} proposed SLO classes`)
