// Requires every service to mount the shared HTTP hardening chain.
//
// The failure this prevents is concrete and already happened once: arda-http
// shipped LimitRequestBody and LimitBodyMiddleware with zero call sites, so the
// limit existed as reviewed, tested code that no request ever passed through.
// The only way that stays fixed is if every service is routed through
// ardahttp.HandlerChain, which is the single place the limit and the panic
// recovery are mounted.
//
// It also checks the other half of the internet-facing surface: without a
// declared MaxHeaderBytes, net/http allows 1MB of request headers per
// connection, which is generous for a JSON API.

import { readFile, readdir } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const errors = []

// Only services that actually serve HTTP are considered. The check keys off
// http.Server rather than a hand-maintained list, so a new service is covered
// the day it is added instead of the day someone remembers this file.
const mtas = []
for (const service of await readdir(join(root, "apps"), { withFileTypes: true })) {
  if (!service.isDirectory()) continue
  const cmdDir = join(root, "apps", service.name, "cmd", service.name, "main.go")
  let source
  try {
    source = await readFile(cmdDir, "utf8")
  } catch {
    continue
  }
  if (!/http\.Server\{/.test(source)) continue
  mtas.push({ service: service.name, source })
}

if (mtas.length === 0) {
  errors.push("no service with an http.Server was found; the check cannot be verifying anything")
}

for (const { service, source } of mtas) {
  // 1. The shared chain, so the body limit and recovery cannot be bypassed by
  //    composing a handler inline.
  if (!/ardahttp\.HandlerChain\(/.test(source)) {
    errors.push(
      `${service}: main.go builds its handler without ardahttp.HandlerChain, so the ` +
        "request-body limit and panic recovery are not mounted",
    )
  }

  // 2. MetricsMiddleware must only be reachable through HandlerChain now.
  const direct = source.match(/ardahttp\.MetricsMiddleware\(/g) ?? []
  if (direct.length > 0) {
    errors.push(
      `${service}: calls ardahttp.MetricsMiddleware directly (${direct.length} site(s)). ` +
        "Use ardahttp.HandlerChain so the body limit is not skipped.",
    )
  }

  // 3. A declared header budget.
  if (!/MaxHeaderBytes:/.test(source)) {
    errors.push(
      `${service}: http.Server has no MaxHeaderBytes; net/http then allows 1MB of ` +
        "headers per connection, which is far more than a JSON API needs",
    )
  }
}

// The library itself must keep exporting the chain, or the services above would
// be calling something that does not exist.
const chain = await readFile(join(root, "libs/go/arda-http/handler_chain.go"), "utf8").catch(() => "")
if (!/func HandlerChain\(/.test(chain)) {
  errors.push("libs/go/arda-http/handler_chain.go does not define HandlerChain")
}
if (!/LimitBodyMiddlewareByPrefix\(/.test(chain)) {
  errors.push("HandlerChain does not mount LimitBodyMiddlewareByPrefix")
}
if (!/RecoveryMiddleware\(/.test(chain)) {
  errors.push("HandlerChain does not mount RecoveryMiddleware")
}

if (errors.length) {
  console.error(errors.join("\n"))
  process.exit(1)
}

console.log(
  `Body limit OK: ${mtas.length} services mount ardahttp.HandlerChain with a body limit, ` +
    "panic recovery and an explicit MaxHeaderBytes",
)
