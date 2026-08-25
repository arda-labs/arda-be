import crypto from "node:crypto"
import { AsyncLocalStorage } from "node:async_hooks"
import express, { type NextFunction, type Request, type Response } from "express"
import { HttpAgent, type RunAgentInput } from "@ag-ui/client"
import { CopilotRuntime } from "@copilotkit/runtime/v2"
import { createCopilotEndpointSingleRouteExpress } from "@copilotkit/runtime/v2/express"

const assistantPermission = "ai.assistant.use"
const serviceTokenVersion = "v1"
const maxClockSkewSeconds = 30
const serviceTokenTTLSeconds = 60

type RuntimeConfig = {
  port: number
  aiServiceUrl: string
  serviceAuthSecret: string
}

type RequestContext = {
  headers: Record<string, string>
}

const requestContext = new AsyncLocalStorage<RequestContext>()

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim() ?? ""
  if (!value) throw new Error(`${name} is required`)
  return value
}

function loadConfig(): RuntimeConfig {
  const serviceAuthSecret = requiredEnv("ARDA_SERVICE_AUTH_SECRET")
  if (serviceAuthSecret.length < 32) {
    throw new Error("ARDA_SERVICE_AUTH_SECRET must be at least 32 characters")
  }

  return {
    port: Number.parseInt(process.env.PORT ?? "8080", 10),
    aiServiceUrl: (process.env.AI_SERVICE_URL ?? "http://ai-service:8080").replace(/\/$/, ""),
    serviceAuthSecret,
  }
}

function base64UrlEncode(value: string | Buffer): string {
  return Buffer.from(value).toString("base64url")
}

function signServiceToken(secret: string, source: string, audience: string): string {
  const issuedAt = Math.floor(Date.now() / 1000)
  const claims = {
    v: serviceTokenVersion,
    src: source,
    aud: audience,
    iat: issuedAt,
    exp: issuedAt + serviceTokenTTLSeconds,
    nonce: base64UrlEncode(crypto.randomBytes(16)),
  }
  const payload = base64UrlEncode(JSON.stringify(claims))
  const signature = crypto
    .createHmac("sha256", secret)
    .update(`${serviceTokenVersion}.${payload}`)
    .digest("base64url")
  return `${serviceTokenVersion}.${payload}.${signature}`
}

function verifyServiceToken(token: string, secret: string, expectedAudience: string): boolean {
  const parts = token.split(".")
  if (parts.length !== 3 || parts[0] !== serviceTokenVersion) return false

  const expectedSignature = crypto
    .createHmac("sha256", secret)
    .update(`${parts[0]}.${parts[1]}`)
    .digest("base64url")
  const actual = Buffer.from(parts[2])
  const expected = Buffer.from(expectedSignature)
  if (actual.length !== expected.length || !crypto.timingSafeEqual(actual, expected)) return false

  try {
    const claims = JSON.parse(Buffer.from(parts[1], "base64url").toString("utf8")) as {
      v?: string
      src?: string
      aud?: string
      iat?: number
      exp?: number
      nonce?: string
    }
    const now = Math.floor(Date.now() / 1000)
    return (
      claims.v === serviceTokenVersion &&
      claims.src === "auth-gateway" &&
      claims.aud === expectedAudience &&
      typeof claims.iat === "number" &&
      typeof claims.exp === "number" &&
      typeof claims.nonce === "string" &&
      claims.iat <= now + maxClockSkewSeconds &&
      claims.exp > now
    )
  } catch {
    return false
  }
}

function hasPermission(value: string): boolean {
  return value
    .split(",")
    .map((permission) => permission.trim())
    .includes(assistantPermission)
}

function headerValue(request: Request, name: string): string {
  const value = request.header(name)
  return typeof value === "string" ? value.trim() : ""
}

function authenticatedContext(config: RuntimeConfig, request: Request): RequestContext | null {
  const serviceToken = headerValue(request, "x-service-auth")
  if (!verifyServiceToken(serviceToken, config.serviceAuthSecret, "ai-runtime")) return null
  if (headerValue(request, "x-auth-checked") !== "true") return null
  if (!headerValue(request, "x-user-id") || !headerValue(request, "x-tenant-id")) return null
  if (!hasPermission(headerValue(request, "x-permissions"))) return null

  const forwardedHeaders = [
    "x-auth-checked",
    "x-user-id",
    "x-actor-user-id",
    "x-target-user-id",
    "x-user-subject",
    "x-username",
    "x-user-email",
    "x-nickname",
    "x-tenant-id",
    "x-org-id",
    "x-user-org-ids",
    "x-roles",
    "x-permissions",
    "x-global-roles",
    "x-global-permissions",
    "x-global-admin",
    "x-user-group-ids",
    "x-session-id",
    "x-auth-version",
    "x-auth-time",
    "x-auth-risk",
  ]
  const headers: Record<string, string> = {}
  for (const name of forwardedHeaders) {
    const value = headerValue(request, name)
    if (value) headers[name] = value
  }
  return { headers }
}

function rewriteAgentInput(body: string): string {
  let input: RunAgentInput
  try {
    input = JSON.parse(body) as RunAgentInput
  } catch {
    return body
  }

  const inputWithTool = input as RunAgentInput & { tool?: unknown }
  const forwardedProps = input.forwardedProps as { ardaTool?: unknown } | undefined
  if (!inputWithTool.tool && forwardedProps?.ardaTool && typeof forwardedProps.ardaTool === "object") {
    // The Go service remains the authority for tool allowlists and permissions.
    // This only adapts the UI hint to the existing Go input contract.
    inputWithTool.tool = forwardedProps.ardaTool
  }
  return JSON.stringify(input)
}

function createAgent(config: RuntimeConfig): HttpAgent {
  return new HttpAgent({
    agentId: "arda-assistant",
    url: `${config.aiServiceUrl}/api/ai/agent`,
    fetch: async (url, init) => {
      const context = requestContext.getStore()
      if (!context) throw new Error("authenticated request context is missing")

      const headers = new Headers(init.headers)
      for (const [name, value] of Object.entries(context.headers)) headers.set(name, value)
      headers.set("x-service-auth", signServiceToken(config.serviceAuthSecret, "ai-runtime", "ai-service"))
      headers.delete("authorization")
      headers.delete("cookie")

      const body = typeof init.body === "string" ? rewriteAgentInput(init.body) : init.body
      return fetch(url, { ...init, headers, body })
    },
  })
}

function requireAuthenticatedRequest(config: RuntimeConfig) {
  return (request: Request, response: Response, next: NextFunction) => {
    const context = authenticatedContext(config, request)
    if (!context) {
      response.status(401).json({ error: "ai_runtime.auth_required" })
      return
    }
    requestContext.run(context, next)
  }
}

function main() {
  const config = loadConfig()
  const app = express()
  app.disable("x-powered-by")

  app.get("/health/live", (_request, response) => response.json({ status: "ok" }))
  app.get("/health/ready", (_request, response) => response.json({ status: "ok" }))

  const runtime = new CopilotRuntime({
    agents: { "arda-assistant": createAgent(config) },
    debug: false,
  })

  app.use(
    "/api/copilotkit",
    requireAuthenticatedRequest(config),
    createCopilotEndpointSingleRouteExpress({
      runtime,
      basePath: "/api/copilotkit",
    }),
  )

  app.listen(config.port, "0.0.0.0", () => {
    console.log(`ai-runtime listening on ${config.port}`)
  })
}

main()
