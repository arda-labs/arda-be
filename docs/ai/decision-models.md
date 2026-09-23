# Decision models

Jev is a typed decision provider, separate from the conversation model. It
uses OpenCode Zen `POST https://opencode.ai/zen/v1/systemone`; the chat
provider continues to use chat-completions. Jev IDs are rejected in chat
profile creation, adding models, applying a model and connection tests.

## Tenant configuration

AI Settings has independent Conversation and Decision tabs. Decision settings
are stored in `public.ai_decision_settings`, one row per tenant; there is no
dependency on the applied conversation profile. Defaults: disabled,
`jev-1.13-free`, minimum confidence 0.8. The paid `jev-1.13` can be selected
explicitly. No automatic paid upgrade. The free offer is time-limited.

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

References: [OpenCode Zen](https://opencode.ai/docs/en/zen/#jev),
[TypeSafe API](https://docs.typesafe.ai/api),
[Confidence](https://docs.typesafe.ai/confidence).
