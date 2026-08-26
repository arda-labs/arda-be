# Performance Baseline & Cost Model — AI Service

Status: **Required before Code Mode production rollout**. Defines token cost
targets, latency budgets, quota thresholds, and monitoring requirements for the
`ai-service` in both direct-tool and Code Mode configurations.

---

## 1. Cost & Latency Model

### 1.1 Direct Tool Mode (current baseline)

Per-turn token breakdown for a typical CRM query:
("Cho tôi biết thông tin khách hàng ABC")

```
System prompt:               ~300 tokens
Conversation history (20):   ~2 000 tokens
User message:                ~20 tokens
Tool definitions (3 tools):  ~600 tokens
──────────────────────────────────────────
LLM input total:             ~2 920 tokens

LLM output (text + tool call): ~150 tokens
Tool result (1 tool):           ~200 tokens
Final LLM output (response):    ~200 tokens
──────────────────────────────────────────
LLM output total:            ~350 tokens

Total per turn:              ~3 270 tokens
```

Latency breakdown (target):
```
Gateway auth check:           ~10 ms
DB: start run + history:      ~20 ms
LLM turn 1 (stream):         ~1 500 ms (TTFT ~400 ms)
Tool execution (CRM):         ~200 ms
LLM turn 2 (stream):         ~800 ms
DB: finish run:               ~15 ms
──────────────────────────────────────
Total:                        ~2 545 ms
```

### 1.2 Code Mode — search + execute (target)

Per-turn token breakdown for a complex cross-domain query:
("Liệt kê khách hàng rủi ro cao và tổng hóa đơn quá hạn")

```
System prompt:               ~300 tokens
Conversation history (20):   ~2 000 tokens
User message:                ~30 tokens
Tool definitions (2 meta):   ~300 tokens   ← 50% reduction vs direct tools
──────────────────────────────────────────
LLM input turn 1:            ~2 630 tokens

search() result (3 methods): ~400 tokens
LLM output (script):         ~200 tokens
execute() result (compact):  ~500 tokens
LLM input turn 2 (total):    ~3 730 tokens
LLM output (final response): ~300 tokens
──────────────────────────────────────────
Total:                        ~4 030 tokens (for 2-domain query)
```

**Key insight:** Code Mode uses ~23% more tokens per complex query than direct
tools on a per-turn basis — but replaces 3+ sequential single-tool turns.
Equivalent 3-tool sequential query would cost ~9 000+ tokens (3× the single
turn). Code Mode wins on complex queries.

**Break-even point:** 2+ domain calls → Code Mode is cheaper. Single-domain
simple queries → direct tool is cheaper (fewer tokens, less latency).

### 1.3 Targets & Alerts

| Metric | Target | Alert threshold |
|:---|:---:|:---:|
| Tokens per turn (avg, direct) | < 3 500 | > 5 000 |
| Tokens per turn (avg, Code Mode) | < 5 000 | > 8 000 |
| TTFT (time-to-first-token) | < 600 ms p95 | > 1 500 ms |
| Total run latency (simple) | < 3 s p95 | > 6 s |
| Total run latency (complex) | < 8 s p95 | > 15 s |
| Sandbox execution time | < 2 s p95 | > 3 s (hard limit) |
| Cost per 1 000 runs | < \$X | > 2×\$X (TBD post-provider selection) |
| Sandbox quota exceeded rate | < 2% | > 10% |
| Static script rejection rate | < 1% | > 5% |

---

## 2. Provider Budget Controls

### 2.1 Per-tenant monthly token budget

To prevent runaway costs from a single tenant:

```go
// Enforced in ai-service before calling the model provider
type TenantBudget struct {
    MonthlyTokenLimit int64  // configured per plan tier
    CurrentMonthUsage int64  // read from ai_usage_monthly aggregate table
}
```

When a tenant exceeds 90% of their monthly budget, the service:
1. Continues processing but adds a `X-Budget-Warning: true` header to responses.
2. At 100%, rejects new runs with HTTP 402 and error code `ai.budget_exceeded`.

### 2.2 Per-run token cap

The model provider client enforces a `max_tokens` parameter per completion
request. Default: **2 048 output tokens**. This prevents a single runaway
generation from consuming the full context window.

### 2.3 Provider timeout & circuit breaker

| Config | Value |
|:---|:---|
| Provider HTTP timeout | 30 s per streaming chunk window |
| Max consecutive provider failures | 5 |
| Circuit breaker open duration | 60 s |
| Fallback behavior | Return `ai.model_unavailable` with `retryable: true` |

---

## 3. Latency Monitoring — Path Decomposition

The SLO dashboard must expose latency **by segment** separately to avoid
misdiagnosing LLM slowness as a gateway or domain service issue:

```
browser → gateway (auth)         Segment: gateway_auth_ms
gateway → ai-service (network)   Segment: gateway_ai_network_ms
ai-service → DB (history load)   Segment: db_history_ms
ai-service → model (TTFT)        Segment: model_ttft_ms
ai-service → model (total)       Segment: model_total_ms
ai-service → sandbox (execute)   Segment: sandbox_exec_ms
ai-service → domain API          Segment: domain_api_ms (per call)
ai-service → DB (write)          Segment: db_write_ms
```

Each segment is recorded in `ai_tool_executions` (for tool/sandbox calls) and
in structured logs (for run-level segments). OpenTelemetry spans propagate
`X-Request-Id` and `X-Trace-Id` from gateway through ai-service and into
domain API calls.

---

## 4. Sandbox-Specific Performance Rules

### 4.1 Concurrent VM Limit

Maximum **8 sandbox VMs per pod** running simultaneously. Requests beyond this
limit are queued in a bounded channel (capacity 16). If the queue is full, the
request fails fast with `ai.sandbox_unavailable` rather than blocking.

Why 8? At 32 MiB per VM, 8 VMs consume up to 256 MiB of memory on the JVM
heap equivalent. With pod memory limits of 512 MiB, this leaves 256 MiB for
the rest of the Go process.

### 4.2 VM Pooling (future optimization)

In Phase 2 (after initial rollout), consider pre-warming a pool of blank Goja
VMs to eliminate the sub-millisecond startup cost on hot paths. The pool should
be bounded to 4 idle VMs per pod. Implementation is not required in Phase 2
POC but should be benchmarked.

### 4.3 Domain API Latency Budget inside Sandbox

Each SDK method call inside the sandbox counts against the **3-second total
sandbox timeout**. Individual domain API calls have their own timeouts (per
`CatalogEntry.TimeoutMs`), but the sandbox interrupt fires regardless.

Recommendation: set domain API timeouts to **1 500 ms** inside the sandbox
(vs. 3 000 ms for direct tool calls) so that two sequential domain calls still
fit within the 3-second budget.

---

## 5. Baseline Measurement Plan

Before Code Mode launches, capture a 2-week baseline of direct-tool mode metrics:

1. **Deploy with metrics** but Code Mode disabled (`AI_ENABLE_CODE_MODE=false`).
2. Collect:
   - Token histograms per turn (p50, p75, p95, p99)
   - Run latency breakdown (all 8 segments above)
   - Tool success/failure/timeout rates
   - Sandbox metrics (all zero in direct-tool mode — establishes baseline)
3. After Code Mode launch, compare the same metrics with `AI_ENABLE_CODE_MODE=true`
   on 10% of traffic (feature flag canary).

**Success criteria for Code Mode canary:**
- p95 run latency for complex (2+ domain) queries decreases vs. direct-tool equivalent
- Token cost per run does not increase by more than 30% on average
- Sandbox quota exceeded rate < 2%
- No increase in domain service error rates (amplification check)
