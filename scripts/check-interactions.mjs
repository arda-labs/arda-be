import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const path = join(root, "contracts", "interactions", "arda-interactions-v1.json")
const document = JSON.parse(await readFile(path, "utf8"))
const required = ["id", "source", "target", "protocol", "operation", "timeout_ms", "retry", "identity", "context", "source_location"]
const allowedProtocols = new Set(["http", "grpc", "nats", "s3", "zeebe"])
const allowedRetries = new Set(["none", "bounded-idempotent", "consumer-redelivery"])
const seen = new Set()
const errors = []

if (document.version !== 1) errors.push("version must be 1")
if (document.policy?.deadline_required !== true) errors.push("policy.deadline_required must be true")
if (document.policy?.identity_required !== true) errors.push("policy.identity_required must be true")
if (document.policy?.trace_context_required !== true) errors.push("policy.trace_context_required must be true")
if (!Array.isArray(document.interactions) || document.interactions.length === 0) errors.push("interactions must be a non-empty array")

for (const interaction of document.interactions ?? []) {
  for (const field of required) {
    if (!(field in interaction)) errors.push(`${interaction.id ?? "<unknown>"}: missing ${field}`)
  }
  if (seen.has(interaction.id)) errors.push(`duplicate interaction id: ${interaction.id}`)
  seen.add(interaction.id)
  if (!allowedProtocols.has(interaction.protocol)) errors.push(`${interaction.id}: unsupported protocol ${interaction.protocol}`)
  if (!allowedRetries.has(interaction.retry)) errors.push(`${interaction.id}: unsupported retry policy ${interaction.retry}`)
  if (!Number.isInteger(interaction.timeout_ms) || interaction.timeout_ms <= 0) errors.push(`${interaction.id}: timeout_ms must be positive integer`)
  if (!Array.isArray(interaction.context) || !interaction.context.includes("request_id") || !interaction.context.includes("traceparent")) {
    errors.push(`${interaction.id}: context must include request_id and traceparent`)
  }
  if (interaction.protocol === "grpc" && interaction.identity !== "mtls-and-signed-service-token") {
    errors.push(`${interaction.id}: gRPC identity must require mTLS and signed service token`)
  }
}

if (errors.length) {
  console.error(errors.join("\n"))
  process.exit(1)
}

console.log(`Interaction policy OK: ${document.interactions.length} calls`)
