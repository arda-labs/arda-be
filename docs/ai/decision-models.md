# Decision models

Jev is a typed decision provider, separate from the conversation model. It
uses OpenCode Zen `POST https://opencode.ai/zen/v1/systemone`; the chat
provider continues to use chat-completions. Jev IDs are rejected in chat
profile creation, adding models, applying a model and connection tests.

## Tenant configuration

AI Settings has independent Conversation and Decision tabs. Decision settings
are stored in `public.ai_decision_settings`, one row per tenant; there is no
dependency on the applied conversation profile. Defaults: disabled,
`jev-1.13-free`, minimum confidence 0.8. Decision model IDs are not fixed: any
System One model ID matching the shared pattern
(`^[A-Za-z0-9][A-Za-z0-9._:/_-]{0,127}$`, enforced by
`internal/decision.Settings.Valid` and the `ai_decision_settings` CHECK) can be
configured; the settings connectivity test is the operator's verification step.
No automatic paid upgrade. The free offer is time-limited.

- `GET /api/ai/settings/decision` returns settings and `has_api_key`, never a key.
- `PUT` saves. Omit `api_key` to preserve it, supply a value to rotate, or an
  empty string to clear it (while disabling routing).
- `POST /api/ai/settings/decision/test` accepts the same draft settings and
  evaluates a fixed Vietnamese loan-report sample. It never saves or enables
  the draft. A successful connection does not certify semantic accuracy.

All endpoints use the existing gateway `ai-settings-read/write` admin policy.
Keys require `enc:v1` encryption, including a database constraint. The preset
endpoint is fixed (no tenant-supplied URLs); the deployment URL allowlist still
applies. HTTP redirects are rejected. API keys and provider error bodies never
appear in responses, model state or logs.

## Runtime

One bounded (3 s), non-retrying request per new conversational run, after quota
reservation. Jev receives only the latest user message and at most one prior
user message, with transcript sanitization and byte limits. It never receives
raw tool data, actor metadata or the SDK catalog. Approval resumes do not
reclassify. Two independent Choice questions are batched: task group and
report topic. Code validates enums, probabilities and confidence before use.

High-confidence results select server-owned guidance for reporting,
LOAN_PORTFOLIO or knowledge retrieval. Specialized report guidance avoids
catalog rediscovery for loan portfolios, asks for a missing period, stops on
empty data and avoids speculative period/label lookups. A final tool-free
model round is reserved for evidence-based synthesis. These are focused
instructions, not a deterministic report executor; model adherence and routing
quality still require end-to-end evaluations.

Disabled, low-confidence, general, invalid or unavailable decisions leave
the existing conversation path unchanged. Routing grants no capabilities,
changes no permissions and never enables Act mode or skips approvals.
Decision model/skill/token usage is persisted separately in
`ai_runs.decision_usage`; decision tokens count towards the run's quota without
being attributed to the chat model. Structured routing logs contain run ID,
skill, model, latency and confidence, not user messages.

## Evaluation and rollout

Apply the additive migration before serving the new code. Deploy BE before
the MFE settings tab. Configure and test a Zen key in the tenant UI, then
enable explicitly. No deployment, key provisioning or activation is implied
by a source change. Disabling the decision setting restores normal chat.

Evaluate Vietnamese questions with/without periods, follow-ups, no-data
reports, mutations, unrelated requests, model errors and low confidence.
Compare routing accuracy, verified answer correctness, tool rounds, p95
latency and total tokens against the same chat model without Jev. The 0.8
threshold is an initial setting, not a measured accuracy guarantee.

### Routing golden set (dev)

`scripts/ai-dev-corpus/routing-evaluation-set.yaml` (bundled copy in
`apps/ai-service/eval/`, kept in sync by `scripts/check-ai-eval-set.mjs`) is the
offline routing golden set. `cmd/ai-eval -mode=routing` calls the System One
provider directly with the production state built by `decision.BuildState`, so
it needs no tenant chat model and no ai-service hop:

```powershell
# from arda-be/apps/ai-service
$env:AI_EVAL_DECISION_API_KEY="<zen key>"
$env:AI_EVAL_ROUTING_ARTIFACT="$env:TEMP\routing.json"   # optional JSON artifact
go run ./cmd/ai-eval -mode=routing
```

Env: `AI_EVAL_ROUTING_SET` (defaults to the canonical dev set),
`AI_EVAL_DECISION_MODEL` (default `jev-1.13-free`),
`AI_EVAL_DECISION_MIN_CONFIDENCE`, `AI_EVAL_ROUTING_STRICT` (off by default;
when on it gates on `AI_EVAL_ROUTING_MIN_ACCURACY` and
`AI_EVAL_ROUTING_MAX_CRITICAL_FP`), `AI_EVAL_ROUTING_ARTIFACT`.

The report scores accuracy, macro F1, confusion, per-tag accuracy, per-skill
false positives, critical `must_not_route` violations, confidence buckets,
p50/p95, tokens and an offline threshold sweep (0.50 → 0.95) that
re-thresholds the same provider answers with `Result.Skill` instead of asking
again.

Baseline (2026-09-23, `jev-1.13-free`, 41 cases, three runs after the set fix):
accuracy 0.951–0.976, macro F1 0.936–0.982, critical FP 0/41 in every run,
p50 0.53–0.57 s, p95 0.61–0.90 s, ~21k/2.9k tokens. The consistent failure is
`followup-confirm` ("Phân tích dư nợ theo nhóm nợ" + "ừ đúng rồi"): a bare
acknowledgement falls to `general` below the threshold, so follow-up handling is
the first candidate for a routing guidance change. Intermittent failures are
short period follow-ups and one topic over-promotion (`report-total-balance`
routed to `loan_portfolio`); mutations, ambiguous and injection cases never
routed to a specialized skill. The sweep now uses the production threshold
values and reads best at 0.80 (last run: accuracy 0.976, precision 0.989, recall
0.977, coverage 0.463), so the default threshold stays 0.80 until a larger
labelled set says otherwise.

### Answer judge (report-only)

`cmd/ai-eval -mode=answer` can score every answer with the Jev judge
(`internal/evaluation/judge.go`), one batched System One request per case:

| Env | Default | Purpose |
|---|---|---|
| `AI_EVAL_JUDGE` | off | set to `jev` to enable |
| `AI_EVAL_JUDGE_API_KEY` | — | required when enabled |
| `AI_EVAL_JUDGE_MODEL` | `jev-1.13-free` | judge model |
| `AI_ANSWER_EVAL_ARTIFACT` | — | JSON artifact with per-case answers and judge values |
| `AI_EVAL_JUDGE_STRICT` | off | gates on `AI_EVAL_JUDGE_MIN_GROUNDED` / `AI_EVAL_JUDGE_MIN_ANSWERS` (default 0.8) |

Judge questions are server-owned (`grounded`, `answers_question`, and
`correct_abstention` for `allow_no_answer` cases); state carries a ≤2 KB answer
and ≤8 KB of citation-anchored evidence, framed as untrusted data. Judge errors
are recorded per case (`judge_error`) and never change pass/fail. Calibration
against human labels (precision/recall/F1 per dimension) is documented in
`judge-calibration.md`; strict stays off until that gate passes.

### Routing OFF vs ON (end-to-end)

Run the same answer set twice — decision routing disabled, then enabled — and
compare. Answer mode reports `tool_calls_total`/`tool_calls_avg` (counted from
`TOOL_CALL_START` events), `p50_ms`/`p95_ms` and the judge rates; token totals
come from `ai_runs`/analytics, not the SSE stream. Toggle the tenant setting
(`PUT /api/ai/settings/decision`), not the deployment.

First live comparison (2026-09-23, dev corpus, 13 cases,
`opencode-go/deepseek-v4.1-flash`; one run per arm — directional, not a gate):

| Metric | OFF | ON |
|---|---|---|
| Answer pass rate | 1.000 | 1.000 |
| Grounded rate | 0.808 | 0.759 |
| Answers-question rate | 0.938 | 0.941 |
| Correct-abstention rate | 0.94 | 0.89 |
| Tool calls / case | 1.54 | 1.31 |
| Chat tokens (prompt / completion) | 173,478 / 7,874 | 151,056 / 5,860 |
| Chat tokens / case | 13,950 | 12,070 |
| p50 latency | 7.3 s | 7.3 s |
| p95 latency | 20.7 s | 13.0 s |

Routing classification itself takes 0.5–0.6 s and ~525 input tokens per run
(`ai_runs.decision_usage`, `jev-1.13-free`); those tokens count against the run
quota but not against the chat model. Verdict: routing ON cut tool calls ≈15%,
chat tokens ≈13% and p95 ≈37% with an equal pass rate; the grounded/abstention
differences are within single-run noise. Keep routing tenant-configurable (off
unless a tenant enables it) and revisit with repeated runs before making it a
default — routing accuracy alone is not the acceptance metric.

References: [OpenCode Zen](https://opencode.ai/docs/en/zen/#jev),
[TypeSafe API](https://docs.typesafe.ai/api),
[Confidence](https://docs.typesafe.ai/confidence).
