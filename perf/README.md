# API performance harness

The harness uses a fixed arrival rate and sends requests directly to the target
API. It separates `http_req_waiting` (time to first byte/server waiting) from
total request time and does not print or store the authenticated cookie.

Example:

```bash
k6 run -e 'TARGET_URL=https://api.arda.io.vn/api/admin/permissions?page=1&per_page=10' \
  -e TARGET_RPS=50 -e TEST_DURATION=2m -e SESSION_COOKIE="$SESSION_COOKIE" \
  perf/api-load.js
```

Record achieved iterations, dropped iterations, errors, p50/p95/p99 waiting,
Cloudflare timing, origin/gateway timing and database/upstream timing separately.
`TARGET_RPS` is the requested rate, not proof that the system achieved that rate.
Use a non-production session or a dedicated read-only test identity.
