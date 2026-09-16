# Audit and observability

## Two audit planes

IAM remains the owner of authentication and authorization audit events such as
login, session, permission, MFA, and security decisions. The AI service owns
AI operational records such as runs, retrievals, tool calls, approvals, model
latency, and outcome.

The planes are correlated by request ID, trace ID, actor ID, tenant ID, run ID,
and conversation ID. Do not duplicate or mutate IAM audit rows from AI service
code. Publish a versioned event when a security-relevant AI action needs to be
consumed by IAM/security tooling.

## Minimum AI audit events

- conversation created/resumed/closed;
- run started/finished/failed/cancelled;
- retrieval attempted/denied/completed, with source IDs and redacted counts;
- tool requested/allowed/denied/started/completed/failed;
- approval requested/approved/rejected/expired/consumed;
- provider error, timeout, budget rejection, or safety refusal;
- data export/deletion/retention action.

Each event includes event ID, timestamp, actor, tenant, request/trace/run IDs,
tool/source/version, policy result, outcome, latency, and a redacted details
object. Never log raw tokens, full sensitive tool payloads, or hidden reasoning.

## Metrics

The machine-readable registry is
`contracts/observability/arda-observability-v1.json`; the `arda_ai_*` families
listed there are enforced by `scripts/check-observability-contract.mjs` against
`apps/ai-service/internal/handler/metrics.go`. Track by service, route, tool,
tenant class, and provider—not by raw prompt:

- request/run rate, success, error, cancellation, and disconnect rate;
- time to first event and total run latency;
- model token usage/cost budget rejection;
- tool allow/deny/error/timeout/retry rates;
- approval wait, expiry, rejection, and replay rates;
- retrieval latency, empty result, citation, ACL-denial, and stale-source rate;
- context size, truncation, and redaction counters.

**Implementation status (2026-09-17):** fifteen `arda_ai_*` families are
implemented and rendered by `RenderAIMetrics`: runs, tool executions, LLM
tokens, run duration, provider probes, model errors, the two citation-guard
counters, the A3 segment families — `arda_ai_model_ttft_seconds`,
`arda_ai_model_duration_seconds`, `arda_ai_retrieval_embed_seconds`,
`arda_ai_retrieval_search_seconds`, `arda_ai_event_publish_failures_total` —
and the A6 context families `arda_ai_prompt_bytes` and
`arda_ai_context_truncated_total`. Contract:
`contracts/observability/arda-observability-v1.json` (15 AI metrics).

Still missing: alert *delivery* (no Alertmanager/Telegram channel — firing
alerts are logged by vmalert and exposed as `vmalert_alerts_firing`), plus
approval-wait/context-size counters and OpenTelemetry spans. The scrape stack
itself landed 2026-09-17: VictoriaMetrics single-node scrapes every service
`/metrics` (`k8s/monitoring/victoria-metrics.yaml`) and vmalert evaluates the
Arda AI rules. Tracked as audit-2026-09 A3.

## Tracing

Propagate request/trace context from the gateway through AI service, tools,
domain calls, provider calls, and persistence. Keep provider spans scrubbed of
prompt/message content by default; record sizes, model IDs, and hashes or
references only where needed for debugging.

**Implementation status (2026-09):** not implemented. `apps/ai-service` has no
OpenTelemetry instrumentation; request IDs propagate and audit rows record
durations, but there are no spans. Closing this is audit-2026-09 item A3; do not
treat this section as deployed behaviour.

## SLO starting point

Do not commit production SLO numbers until the service has representative
baseline data. The
initial dashboard should nevertheless expose the full path separately:

```text
browser -> gateway -> AI service -> retrieval/tool -> provider -> database
```

This prevents model latency, gateway auth latency, and domain API latency from
being misdiagnosed as one number.
