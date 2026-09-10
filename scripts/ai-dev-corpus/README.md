# AI dev corpus (RAG)

Synthetic Vietnamese corpus and golden questions used to exercise the
knowledge pipeline end to end before a real approved corpus exists. The
corpus belongs to tenant `00000000-0000-0000-0000-000000000010`.

## Contents

| File | Role |
|---|---|
| `docs/*.md` | 12 synthetic policy/process documents (sources `src-2`..`src-13`) |
| `evaluation-set.yaml` | Golden questions, validated offline by `apps/ai-service/internal/evaluation/devset_test.go` |
| `seed.mjs` | Pushes docs through the real knowledge API (create → version → review → publish) |
| `query-test.mjs` | Ad-hoc retrieval smoke queries |

Generated artifacts (`chunks.csv`, `query-vecs.json`) and one-off scratch
scripts are git-ignored.

## Prerequisites

- A reachable `ai-service` with `ARDA_SERVICE_AUTH_SECRET` matching the
  service (seed script signs workload tokens the same way auth-gateway does).
- An embedding provider for ingestion. Production fails closed without one;
  development can fall back to FTS-only.

## Seed

```bash
SECRET=<ARDA_SERVICE_AUTH_SECRET> BASE=http://127.0.0.1:18080 \
  node scripts/ai-dev-corpus/seed.mjs
```

## Evaluate

From `arda-be/apps/ai-service`:

```bash
AI_EVAL_SET=../../scripts/ai-dev-corpus/evaluation-set.yaml \
AI_EVAL_BASE_URL=http://127.0.0.1:8098 \
AI_EVAL_STRICT=1 \
go run ./cmd/ai-eval
```

Use `AI_EVAL_TENANT` when the seeder targeted a tenant other than the one
hard-coded in the set. The report includes recall@k, citation coverage, hit
counts and per-case pass/fail.

Closing the production quality gate requires replacing this synthetic corpus
with approved tenant documents and re-running the evaluator; see
`docs/ai/evaluation-set.yaml` for the release template.

## Live baseline (K3s, 2026-09-10)

Run through a port-forward to the deployed `ai-service` (production mode,
embedding provider enabled, no reranker):

- 12/12 cases pass with `AI_EVAL_STRICT=1`; recall@10 = 1.0 on answerable cases.
- Distant out-of-corpus and cross-tenant queries return 0 hits (floor 0.35).
- Measured limitation: a *near-domain* hard negative ("làm việc 4 ngày mỗi
  tuần") still returns lexically related chunks above the floor. Retrieval-level
  floors cannot separate that case; it needs answer-level abstention
  (citation/groundedness validator), so it is not part of this retrieval gate.
- Latency: 0.4–6.5 s per query for `top_k=10`; no reranker configured.

To reproduce, port-forward the service and provide the workload secret so the
evaluator can sign requests (`AI_EVAL_SERVICE_SECRET`):

```powershell
kubectl -n arda-app port-forward svc/ai-service 18099:8080
$env:AI_EVAL_BASE_URL="http://127.0.0.1:18099"
$env:AI_EVAL_SERVICE_SECRET="<ARDA_SERVICE_AUTH_SECRET>"
$env:AI_EVAL_SET="../../scripts/ai-dev-corpus/evaluation-set.yaml"
$env:AI_EVAL_TENANT="00000000-0000-0000-0000-000000000010"
$env:AI_EVAL_STRICT="1"
go run ./cmd/ai-eval   # from arda-be/apps/ai-service
```
