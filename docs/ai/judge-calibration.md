# Jev judge calibration

The answer judge (`apps/ai-service/internal/evaluation/judge.go`) scores every
answer with System One `noul` questions: `grounded` (every factual claim is
supported by the evidence), `answers_question` (the answer addresses the
question) and `correct_abstention` (only for `allow_no_answer` cases). It stays
**report-only** until its agreement with human labels is measured.

## Workflow

1. Run the answer eval with the judge and persist the artifact:

   ```powershell
   # from arda-be/apps/ai-service
   $env:AI_ANSWER_EVAL_SET="../../scripts/ai-dev-corpus/answer-evaluation-set.yaml"
   $env:AI_ANSWER_EVAL_ARTIFACT="$env:TEMP\answer-judge.json"
   $env:AI_EVAL_JUDGE="jev"
   $env:AI_EVAL_JUDGE_API_KEY="<zen key>"
   # optional: $env:AI_EVAL_JUDGE_MODEL="jev-1.13-free"
   go run ./cmd/ai-eval -mode=answer
   ```

2. Read `report.cases[]` in the artifact (`answer_excerpt`, `grounded`,
   `answers_question`, `correct_abstention`, `judge_error`) and correct the
   labels in `scripts/ai-dev-corpus/judge-calibration.yaml`. Labels describe
   what the answer *should* be, reviewed by a human.

3. Compare judge against labels (no provider calls):

   ```powershell
   $env:AI_EVAL_ANSWER_ARTIFACT="$env:TEMP\answer-judge.json"
   go run ./cmd/ai-eval -mode=judge-calibrate
   ```

   Per dimension the report gives the confusion matrix (TP/FP/FN/TN),
   precision, recall, F1 and agreement at the 0.5 probability threshold, plus
   `missing_values`, `untracked_cases` and `judge_errors`. `AI_EVAL_USER_ID`
   must be a UUID for answer mode (`ai_runs.actor_user_id` is a uuid column);
   the CLI default is a fixed synthetic UUID.

4. Record the numbers below before proposing any strict gate.

## Strict gate (not enabled)

Only after calibration review:

```powershell
$env:AI_EVAL_JUDGE_STRICT="1"          # default off
$env:AI_EVAL_JUDGE_MIN_GROUNDED="0.8"  # default when strict is on
$env:AI_EVAL_JUDGE_MIN_ANSWERS="0.8"
```

Judge errors are recorded per case and never change pass/fail in report-only
mode; strict mode gates only on the aggregate rates.

## Guardrails

- Judge state carries the question, a ≤2 KB answer and ≤8 KB of
  citation-anchored evidence. Treat all of it as untrusted data: the prompts
  say so explicitly and the provider only returns probabilities.
- Run the judge only on the dev corpus or an explicitly approved tenant until a
  data-flow decision covers tenant documents.
- The judge is an evaluation signal, never an authorization or approval gate.
- Watch `grounded` false positives first: an unsupported claim scored as
  grounded is the expensive error.

## Status

2026-09-23 — first live run, dev corpus answer set (13 cases, tenant
`...010`, `opencode-go/deepseek-v4.1-flash`, judge `jev-1.13-free`), labels
from `scripts/ai-dev-corpus/judge-calibration.yaml`:

| Dimension | Cases | TP | FP | FN | TN | Precision | Recall | F1 | Agreement |
|---|---|---|---|---|---|---|---|---|---|
| grounded | 12 | 11 | 0 | 1 | 0 | 1.000 | 0.917 | 0.957 | 0.917 |
| answers_question | 13 | 13 | 0 | 0 | 0 | 1.000 | 1.000 | 1.000 | 1.000 |
| correct_abstention | 1 | 1 | 0 | 0 | 0 | 1.000 | 1.000 | 1.000 | 1.000 |

Notes:

- `grounded` **false positives = 0**, the property that matters before any
  strict gate; the single false negative is `payment-approval-threshold`
  (judge 0.31 against a grounded answer) — the answer states several approval
  thresholds, so the judge likely saw only part of the evidence.
- 7 FAQ labels were untracked in this run because the artifact came from the
  dev corpus set; run `docs/ai/answer-evaluation-set.yaml` to cover them.
- The evidence selector first shipped as citation-window snippets over raw tool
  JSON and scored `grounded` mean 0.25 on the same corpus. Extracting the
  `content`/`text` chunk fields (both object and `[["key","value"],…]` shapes)
  raised it to 0.76 without changing the judge prompt.
- Answer mode requires `AI_EVAL_USER_ID` to be a UUID (`ai_runs.actor_user_id`
  is a uuid column); the CLI default is a fixed synthetic UUID.

Decision: keep the judge **report-only**; strict gating stays off until a
second run and a larger labelled set confirm these numbers.
