import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const files = {
  service: "apps/notification-service/cmd/notification-service/main.go",
  compose: "docker-compose.yml",
}
const sources = new Map()
for (const [name, file] of Object.entries(files)) {
  sources.set(name, await readFile(join(root, file), "utf8"))
}

const service = sources.get("service")
const compose = sources.get("compose")
const errors = []

if (!service.includes('if cfg.NATSURL == ""')) errors.push("notification service must fail when NATS_URL is missing")
if (/NATS unavailable;.*remain pending/i.test(service)) errors.push("notification service must not silently leave the outbox pending")
if (!service.includes("appevents.NewNATSPublisher(nc)")) errors.push("notification service must initialize the JetStream publisher")
if (!compose.includes("nats:2.10-alpine") || !compose.includes("nats-data:/data")) errors.push("local Compose must provide durable JetStream storage")
if (!compose.includes("NATS_URL: nats://nats:4222")) errors.push("local notification service must use the explicit NATS service")
if (!compose.includes("dockerfile: apps/notification-service/Dockerfile")) errors.push("local notification build must use the monorepo context")

if (errors.length) {
  console.error(errors.join("\n"))
  process.exit(1)
}

console.log("Event runtime invariant OK: notification outbox requires JetStream")
