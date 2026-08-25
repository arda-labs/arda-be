import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const files = [
  "apps/iam-service/internal/policy/enforcer.go",
  "apps/iam-service/internal/middleware/tenant.go",
  "apps/iam-service/cmd/iam-service/main.go",
  "apps/iam-service/internal/transport/http/router.go",
  "apps/auth-gateway/internal/policy/policy.go",
  "apps/auth-gateway/internal/handler/bff_handler.go",
  "apps/auth-gateway/internal/session/redis.go",
  "apps/auth-gateway/cmd/auth-gateway/main.go",
  "apps/finance-service/cmd/finance-service/main.go",
]
const forbidden = [
  /Allowed:\s*true/,
  /Allow by default/i,
  /Allow cross-tenant for admin APIs/i,
  /e\.handler\.ServeHTTP\(w, r\)/,
  /policy enforcement disabled/i,
  /casbin enforcer not available/i,
  /getResp, _ :=/,
  /http\.NewRequest\(/,
  /s\.client\.Get\(ctx, key\)/,
  /ProxyBackendURL/,
  /return fallback, true/,
  /if cfg\.RedisURL != ""/,
  /platform grpc unavailable/,
  /if cfg\.PlatformGRPCAddr != ""/,
]
const violations = []

for (const file of files) {
  const source = await readFile(join(root, file), "utf8")
  for (const pattern of forbidden) {
    if (pattern.test(source)) violations.push(`${file}: forbidden fail-open pattern ${pattern}`)
  }
  if (file.endsWith("internal/transport/http/router.go") && /\/api\/auth\//.test(source)) {
    violations.push(`${file}: IAM must not reintroduce public auth routes; auth-gateway owns the browser auth boundary`)
  }
}

if (violations.length) {
  console.error(violations.join("\n"))
  process.exit(1)
}

console.log(`Security invariant OK: ${files.length} auth boundary files checked`)
