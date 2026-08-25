# Phase 0 Baseline Report

Status: initial evidence; refresh and attach raw artifacts before Phase 1 exit.

Snapshot date: 2026-08-25

## Correctness/tooling baseline already observed

From the preceding repository audit:

- MFE package-boundary check passed.
- MFE TypeScript typecheck passed.
- MFE API client targeted tests passed.
- Targeted backend tests passed.
- All Go workspace modules passed when tested individually.
- Top-level `go test ./...` is not the correct command for the multi-module root;
  per-module testing is required until CI provides a workspace wrapper.

These results are not a Phase 0 gate until the exact commit, commands, environment
and logs are attached to the baseline artifact.

## Public API latency baseline

Observed during the earlier API investigation:

- public authenticated API samples were approximately 123–203 ms for most requests,
  with a higher outlier around 620 ms;
- direct internal Traefik/service path was approximately 3.8–7 ms;
- auth-gateway ClusterIP samples were approximately 3.2–17 ms;
- Cloudflare traffic was observed at HKG colo despite the client being in Vietnam.

Interpretation for the refactor:

- public waiting time must be decomposed into client, Cloudflare edge/network,
  tunnel, gateway, service, DB and downstream spans;
- this baseline is evidence for measurement design, not a permanent SLO;
- capacity tests must report completed requests, errors, dropped work, p95/p99 and saturation.

## Current route/contract baseline

- auth-gateway has explicit auth/session routes plus a generic `/api/` proxy.
- Backend now exposes the canonical `/api/...` CRM/workflow families; the
  previously inventoried `/api/v1` aliases were removed after the current FE
  consumer search found no supported callers.
- Existing convention documents describe standard lists/errors, but source consumers
  still contain mixed arrays, list objects, action objects and nested data assumptions.
- FE auth/bootstrap raw fetch calls are classified as explicit Kratos/OAuth
  protocol adapters and retain browser credentials; domain JSON calls use the
  shared transport.

## Security baseline

The audit requires Phase 1 evidence for:

- unknown gateway policy behavior;
- browser-forged identity/org/permission header behavior;
- actor versus target authorization on admin operations;
- tenant/org scope and empty membership behavior;
- OAuth callback/dev path and local token verification;
- recent-auth/MFA/trusted-device decision path;
- internal service identity and header trust;
- secret/cookie/token redaction and rotation state.

## Performance artifact required

Before implementation, store one reproducible artifact per representative class:

```text
public GET list
public GET detail
authenticated admin command
upload-init/complete/download
internal gRPC read/command
event publish/consume
MFE shell cold load and remote navigation
```

Each artifact includes timestamp, commit, environment, request count, concurrency/
RPS target, completed work, dropped work, status/error counts, p50/p95/p99,
component timing and resource saturation.

## Baseline acceptance

- [ ] Raw command/output artifacts are attached or linked privately where sensitive.
- [ ] Public versus origin timing is separated.
- [ ] DB query count/plan and payload size are captured for pilot operations.
- [ ] FE bundle/remote/API/render timings are separated.
- [ ] Security negative tests have a reproducible environment.
- [ ] Existing dirty files are recorded and excluded from baseline claims.
