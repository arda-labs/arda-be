# AI knowledge corpus — docs-as-code pipeline

Pipeline nạp tri thức thật cho Olorin RAG: markdown trong repo → chunk →
embedding → tìm kiếm có citation. Đã chốt (2026-09-14): corpus scope
`global/system` được **auto-approve** bởi `corpus-bot`; tài liệu `tenant` vẫn
review tay qua UI.

## Nguồn nào được index

Quyết định bằng `manifest.yaml` — mỗi glob là một nhóm file; **source identity
= đường dẫn file** (slash, relative tới thư mục manifest), vì vậy citation luôn
trỏ về đúng file trong repo. Thêm nguồn = thêm glob + commit; xóa nguồn khỏi
manifest **không** tự gỡ source (gỡ tay trong AI Center, giữ audit trail).

## Guardrail (bắt buộc, fail-closed)

1. **Scan secret/PII trước mọi API call**: private key, `AKIA…`, `sk-…`,
   `AIza…`, `ghp_…`, dòng `password/token/api_key = …`, số 12 chữ số kiểu CCCD.
   Một match duy nhất → abort toàn bộ run, không publish gì cả.
2. **Idempotent theo content hash**: file không đổi → skip, không tạo version.
3. **Auto-approve có audit**: `owner = docs-team`, review/publish =
   `corpus-bot` (khác owner), `status_history` ghi rõ trong DB.
4. **Không index dữ liệu sống** (CRM/finance/HRM) — dữ liệu sống đi qua tool,
   không vào corpus (roadmap §4.6).

## Chạy

```powershell
# arda-be/
# 1) Dry-run: resolve glob + scan + report, không gọi API
go run ./cmd/knowledge-indexer --manifest scripts/ai-knowledge/manifest.yaml --dry-run

# 2) Dev local (ai-service chạy ở 8098, không cần secret signing)
$env:ARDA_SERVICE_AUTH_SECRET = "<secret 32+ chars>"
go run ./cmd/knowledge-indexer --manifest scripts/ai-knowledge/manifest.yaml --base-url http://127.0.0.1:8098

# 3) Cluster (từ máy có KUBECONFIG): port-forward rồi chạy như trên
kubectl -n arda-app port-forward svc/ai-service 8098:8080
go run ./cmd/knowledge-indexer --manifest scripts/ai-knowledge/manifest.yaml --base-url http://127.0.0.1:8098 --tenant <tenant-id>

# 4) Qua gateway công khai (api.arda.io.vn): cần session của service account
#    corpus — đặt AI_CORPUS_COOKIE, và ARDA_SERVICE_AUTH_SECRET để ký workload.
#    GitHub Actions: workflow_dispatch "knowledge-sync" (mặc định dry-run).
```

Flags: `--actor` (mặc định `corpus-bot`), `--permissions`
(`ai.knowledge.manage,ai.assistant.use`), `--job-timeout-seconds` (120),
`--dry-run`. Env `AI_CORPUS_COOKIE` (chỉ khi gọi qua gateway). Thoát non-zero
khi có file fail — job ingestion timeout **không** bị nuốt thành skip.

## Kiểm chứng sau khi chạy

- Chạy 2 lần liên tiếp: lần 2 báo `skipped` cho mọi file, không tạo version.
- Sửa 1 file → chỉ file đó `updated`; citation giữ version cũ đúng hiệu lực.
- `POST /api/rag/query` câu hỏi bám nội dung file → hit + citation đúng
  `sourceID:heading` (xem `scripts/ai-dev-corpus/query-test.mjs` để smoke).
- Eval gate: chạy `cmd/ai-eval` với golden set thật (đang bổ sung, ≥20 câu).

## Việc còn lại (WS3)

- T3.1 verify schema 3 bảng knowledge trên DB cluster (drift `IF NOT EXISTS`).
- T3.4 nội dung wave 1: product overview từng module (nguồn `.github/profile`
  + README service), mở rộng FAQ.
- T3.5 golden set thật ≥20 câu + ghi kết quả eval.
- T3.6 GitHub Action `workflow_dispatch` (sau khi pipeline ổn định).
