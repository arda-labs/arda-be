import http from "k6/http"
import { check } from "k6"
import { Counter, Trend } from "k6/metrics"

const targetURL = __ENV.TARGET_URL || "https://api.arda.io.vn/api/admin/permissions?page=1&per_page=10"
const targetRPS = Number(__ENV.TARGET_RPS || 10)
const duration = __ENV.TEST_DURATION || "30s"
const cookie = __ENV.SESSION_COOKIE || ""

export const options = {
  scenarios: {
    fixed_rate: {
      executor: "constant-arrival-rate",
      rate: targetRPS,
      timeUnit: "1s",
      duration,
      preAllocatedVUs: Math.max(10, targetRPS),
      maxVUs: Math.max(50, targetRPS * 5),
    },
  },
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    http_req_waiting: ["p(95)<500", "p(99)<1000"],
  },
}

const completed = new Counter("completed_requests")
const waiting = new Trend("server_waiting_ms")

export default function () {
  const headers = {
    Accept: "application/json",
    "X-Request-Id": `k6-${__VU}-${__ITER}`,
  }
  // The authenticated cookie is supplied only through the process environment
  // and is never printed by this script or included in a metric label.
  if (cookie) headers.Cookie = cookie

  const response = http.get(targetURL, { headers, tags: { route: "permissions_list" } })
  waiting.add(response.timings.waiting)
  const ok = check(response, {
    "status is successful or forbidden": (res) => [200, 401, 403].includes(res.status),
  })
  if (ok) completed.add(1)
}
