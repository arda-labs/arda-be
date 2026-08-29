# Lộ trình tiến hóa Agent Platform Arda

> Tài liệu đánh giá & lộ trình đề xuất — **chưa phải quyết định**, phục vụ review.
> Phạm vi: xuyên suốt `arda-be` (ai-service, auth-gateway), `arda-mfe` (packages/ai, shell), `arda-infra`.
> Ngày viết: 2026-08-29. Bối cảnh: spike đã chạy end-to-end 2026-08-26, đối chiếu với pattern của Cloudflare Code Mode, Anthropic code-exec-with-MCP, LangGraph interrupt/resume.

## 1. Nguyên tắc bất di bất dịch (không thương lượng)

Mọi thay đổi trong tài liệu này phải tôn trọng 3 ràng buộc đã chọn:

1. **Authorization tập trung** — mọi request đi qua `auth-gateway` + `policy.yaml`. Không có component nào (Worker edge, sidecar, SDK client) được nối thẳng tới service mà bypass mô hình này.
2. **Self-hosted k3s** — dữ liệu tenant (khách hàng tài chính) không rời cluster trừ khi có quyết định riêng. Provider LLM ngoài là ngoại lệ duy nhất đã được chấp nhận (chỉ gửi prompt, không gửi raw customer data — xem §3 DLP).
3. **Go-native boundary** — không quay lại adapter Node. Framework agent mới chỉ được tham khảo code, không được kéo runtime mới vào.

## 2. Phân mảnh trách nhiệm — đây KHÔNG phải việc của riêng ai-service

| Mảnh | Repo chịu trách nhiệm | Ghi chú |
|---|---|---|
| Agent loop, tools, sandbox, HITL | `arda-be/apps/ai-service` | Trung tâm, nhưng chỉ ~40% khối lượng còn lại |
| Giao thức streaming FE↔BE | `arda-be` (emit) + `arda-mfe/packages/ai` (consume) | Phải đổi *đồng bộ hai phía*, một mình bên nào đổi cũng vỡ |
| Route + policy cho endpoint AI mới | `arda-be/apps/auth-gateway/configs/policy.yaml` | Mỗi endpoint mới là 1 route mới — bắt buộc |
| Model gateway / proxy | `arda-infra` (nếu chọn AI Gateway) + `ai-service` config | Chỉ đổi `AI_MODEL_BASE_URL`, nhưng cần secret routing |
| Tracing/observability | `ai-service` (emit) + `arda-infra` (thu nhận, dashboard) | |
| RAG / knowledge | `ai-service/internal/knowledge` + pipeline indexing (job) + Postgres pgvector | Không cần repo mới |
| FE chat UX, tool renderers | `arda-mfe/packages/ai` + `apps/shell` | |

**Kết luận quản trị:** mỗi hạng mục dưới đây cần PR ở 2–3 repo cùng lúc. Nên làm theo cột mốc (milestone) có gate, không làm theo repo.

## 3. Mảnh "mượn" số 1 — AI Gateway làm model proxy

### 3.1. Áp dụng được không? — Có, và là mảnh rủi ro thấp nhất

Thực trạng: `ai-service` gọi provider ngoài trực tiếp qua `AI_MODEL_BASE_URL=https://opencode.ai/zen/v1` (Deployment env), per-tenant override qua `ai_tenant_settings`. `model.Client` chỉ cần đổi `baseURL` là đi qua proxy — **không đổi code Go, không đổi FE**.

### 3.2. Kiến trúc đề xuất

```text
ai-service (Go, trong cluster)
   │  POST {gateway-host}/{provider}/v1/chat/completions
   │  Authorization: Bearer <gw-key>
   ▼
Cloudflare AI Gateway  ──►  provider thật (opencode/zen, OpenAI, ...)
   ├── cache (câu hỏi lặp lại, embedding request thì TẮT cache)
   ├── fallback + retry (provider chính lỗi → provider dự phòng)
   ├── logging/analytics (token, chi phí, latency per request)
   └── DLP / Guardrails (quét số thẻ, CCCD, số TK trước khi ra ngoài)
```

- `AI_MODEL_BASE_URL` đổi thành URL của gateway (gateway host + provider path). `ai_tenant_settings.base_url` cũng trỏ qua gateway — như vậy **mọi tenant key đều đi qua một điểm kiểm soát** thay vì bay thẳng ra Internet.
- Fallback/retry: hiện `StreamChat` fail → run FAILED ngay. Gateway cho phép cấu hình provider dự phòng — mức "free" đầu tiên của resilience.
- Analytics per-tenant: gateway log request kèm metadata, đủ để trả lời "tenant X tốn bao nhiêu token tháng này" mà không phải tự build.

### 3.3. Cảnh báo khi áp dụng

1. **Streaming SSE qua gateway phải được verify thật** — cache/guard có thể buffer làm hỏng streaming. Test bằng run thật trước khi cắt.
2. **DLP không thay thế redaction phía mình.** `sanitizeTranscript` (args redacted vào DB) vẫn phải giữ — DLP chỉ che dữ liệu trước khi ra provider, không che trong log/DB nội bộ.
3. **Latency +1 hop ra edge Cloudflare.** Với self-hosted cluster, request đi cluster → Cloudflare → provider. Chấp nhận được cho LLM (độ trễ generation lơn hơn hẳn hop), nhưng nên đo.
4. **Phương án B (nếu không muốn phụ thuộc CF cho luồng model):** LiteLLM proxy tự host trên k3s — cùng đặc tính (unified API, fallback, logging, budget) nhưng nằm trong cluster. Đánh giá theo tiêu chí: DLP có sẵn (CF thắng), vận hành thêm 1 service (LiteLLM thua), chi phí. **Khuyến nghị: thử AI Gateway trước** vì không tốn công vận hành; LiteLLM là kế hoạch dự phòng, không cần quyết ngay.

### 3.4. Việc cần làm (ước lượng ~1–2 ngày)

1. Tạo AI Gateway trên CF account, cấu hình provider + key thật vào gateway (key thật không còn nằm ở ai-service).
2. Bật logging + (tuần 2) DLP rules với mẫu số thẻ/CCCD VN.
3. Đổi `AI_MODEL_BASE_URL` ở Deployment + test SSE streaming qua gateway.
4. Chạy load thử `arda-perf` (nhỏ, 10 req/s) so latency trước/sau.
5. Cập nhật `ai_tenant_settings` flow: tenant base_url phải thuộc allowlist domain gateway (khắc phục luôn lỗ hổng SSRF đã nêu — cho phép chỉ `https://gateway.cloudflare.com/...`).

### 3.5. Thiết kế routing switchable: AI Gateway ↔ LiteLLM ↔ direct (ĐÃ CHỐT)

`model.Client` là OpenAI-compatible (`baseURL + apiKey + modelID`) — cả AI Gateway lẫn LiteLLM đều nói đúng giao thức đó, nên **switch là chuyện env, không phải code**:

```text
CF AI Gateway:  AI_MODEL_BASE_URL=https://gateway.ai.cloudflare.com/v1/<account>/<gw>/openai
LiteLLM:        AI_MODEL_BASE_URL=http://litellm.arda-infra.svc:4000/v1
Direct:         https://opencode.ai/zen/v1   (hiện tại)
```

Thiết kế lại `ai_tenant_settings` (không cần migration schema):

1. **Platform route** — `AI_MODEL_BASE_URL` trong Deployment (`arda-infra`) là điểm chuyển duy nhất. Đổi gateway = đổi env + rollout; tenant không phải làm gì.
2. **Allowlist mới:** env `AI_MODEL_BASE_URL_ALLOWLIST` (comma-separated domain, mặc định = platform gateway URL). Validate **ở cả hai chỗ**: upsert `/api/ai/settings` (reject 400 + audit) và tại use trong `agent.go` trước khi tạo client (defense in depth). Đây đồng thời là bản vá SSRF.
3. **API key tenant** trở thành key scope-gateway — provider key thật nằm ở gateway, ai-service không bao giờ thấy (mã hóa `enc:v1:` hiện có giữ nguyên).
4. Khi chuyển gateway→LiteLLM: thêm domain LiteLLM vào allowlist, đổi env platform, rollout. Tenant rows giữ nguyên, vẫn hợp lệ.

**Quyết định Data-flow: ĐÃ CHỐT (2026-08-29)** — chấp nhận luồng model qua CF account (chỉ prompt, không raw customer data), thiết kế switchable theo §3.5 để quay về LiteLLM self-host bất cứ lúc nào mà không đổi code.

## 4. Mảnh "mượn" số 2 — RAG bằng pgvector trong Postgres

### 4.1. Áp dụng được không? — Có, và không cần bất kỳ dịch vụ mới nào

Thực trạng: pgvector **đã bật** (`20260826140000_enable_pgvector.sql`), bảng `ai_knowledge_sources` / `ai_knowledge_chunks` **đã có** (scope global/tenant/system, effective_from/to), nhưng `knowledge/search.go` chỉ dùng full-text `tsquery` + `ILIKE`. Khoảng trống duy nhất: **chưa có embedding và chưa có vector index**.

### 4.2. Thiết kế đề xuất (giữ trong cluster)

```text
Tài liệu (Confluence/DB nội bộ/markdown) 
   → job indexing (cron/gha, Go CLI trong arda-be) 
       chunk 500-800 token → embed (model multilingual) 
       → upsert ai_knowledge_chunks (embedding vector(1024), tenant_id, scope)
   → ai-service/search.go: hybrid search
       tsquery (hiện có) + ORDER BY embedding <=> $query_vec
       → rerank đơn giản: vector rank + ts_rank
```

- **Embedding model (ĐÃ CHỐT: `@cf/qwen/qwen3-embedding-0.6b`, fallback `@cf/baai/bge-m3`):** cả hai đều 1024 dims (khớp `vector(1024)`), multilingual hỗ trợ tốt tiếng Việt. qwen3 mới hơn, điểm MTEB multilingual cao hơn ở cùng size; bge-m3 battle-tested hơn, 8k context. Chạy trên Cloudflare Workers AI — nội dung tài liệu knowledge gửi ra CF (chấp nhận theo quyết định Data-flow §3.5; tài liệu nhạy cảm tenant xem §4.5).
- **Thiết kế `Embedder` hai provider (ĐÃ CHỐT — hỗ trợ cả CF lẫn self-host):**

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Model() string      // ghi vào ai_knowledge_chunks.embedding_model
    Dimensions() int    // 1024 cho cả hai model đã chọn
}
```

  - **Bây giờ — `WorkersAIEmbedder`:** gọi REST `POST /api.cloudflare.com/client/v4/accounts/<account>/ai/run/@cf/qwen/qwen3-embedding-0.6b` (Bearer CF API token). Endpoint Workers AI **không** OpenAI-compatible nên cần client riêng (~80 dòng). Có thể định tuyến qua AI Gateway để được cache/analytics.
  - **Sau này — `OpenAIEmbedder`:** vLLM/Ollama trên k3s đều expose `/v1/embeddings` chuẩn OpenAI — implementation chỉ là http client thông thường. Khi có máy self-host, switch = đổi env, không đổi code.
  - **Config env:** `AI_EMBEDDING_PROVIDER=workersai|openai`, `AI_EMBEDDING_MODEL`, `AI_EMBEDDING_API_TOKEN` (secret `arda-app-secrets`).
  - **Bất biến:** search chỉ so vector khi `embedding_model` của query khớp chunk; đổi model = chạy lại job re-embed, không bao giờ so chéo hai không gian vector.
- Embedding chạy **offline trong job**, không chạy lúc request. Query-time embed 1 câu là 1 call nhỏ.
- Tenant scoping giữ nguyên logic hiện tại (`scope`, `tenant_id`) — pgvector không thay đổi phân quyền.
- Chi phí index: pgvector HNSW trên vài triệu chunk vẫn nhẹ cho Postgres 18 CNPG hiện có.

### 4.3. Việc cần làm (ước lượng ~3–5 ngày)

1. Migration: cột `embedding vector(1024)` + HNSW index trên `ai_knowledge_chunks` (chọn dimension theo model — quyết định chung với model choice).
2. CLI/job `arda-be/scripts` hoặc subcommand: ingest → chunk → embed → upsert.
3. `search.go`: hybrid query (đã có tsquery, thêm cosine distance), feature flag `AI_KNOWLEDGE_VECTOR=on`.
4. Nguồn dữ liệu đầu tiên: docs sản phẩm/tính năng của chính Arda — assistant trả lời "tính năng X dùng thế nào" trước, sau mới đến tài liệu nghiệp vụ tenant.
5. Đánh giá chất lượng: bộ 20–30 câu hỏi tiếng Việt vàng, so full-text vs hybrid — **đo rồi mới tắt full-text**.

### 4.4. Khi nào KHÔNG cần làm ngay

Nếu use case gần nhất vẫn là *hành động trên dữ liệu sống* (tra cứu khách hàng, tạo giao dịch, duyệt), RAG chưa chặn đường gì — `knowledge.search` tool chỉ chiếm số call nhỏ. Làm RAG khi bắt đầu ["assistant trả lời theo chính sách/sản phẩm"] — quyết định theo sản phẩm, không theo kỹ thuật.

### 4.5. Embedding khi chưa có máy self-host (ĐÃ CHỐT)

Hiện tại không có máy self-host → **dùng Workers AI trước** (hệ thống đã đi qua Cloudflare), thiết kế `Embedder` (§4.2) đảm bảo self-host sau này chỉ là thêm một implementation + đổi env. Tài liệu nhạy cảm của tenant: khi có nhu cầu index mà không được rời cluster, chỉ những tài liệu đó được chuyển sang provider self-host (cột `embedding_model` cho phép co-exist hai provider trong cùng bảng — mỗi chunk biết nó do ai embed).

## 5. Lộ trình 4 cột mốc (thứ tự khuyến nghị — ĐÃ CHỐT: M1 trước)

### M1 — Giao thức stream chuẩn hóa (P0, ~1 tuần, BE + MFE)
Đổi BE emit sang **AI SDK UI Message Stream** (SSE chuẩn mở của Vercel AI SDK — chuẩn mà assistant-ui phía FE hỗ trợ-native).
- BE: `handler/sse.go` + `agent.go` emit event theo spec mới (part-based: `text-delta`, `tool-input-start`, `tool-output`...).
- MFE: xóa `packages/ai/src/adapter.ts` tự viết, dùng transport của assistant-ui/AI SDK. Tool renderer registry giữ nguyên.
- Lợi ích trực tiếp: bỏ ~150 dòng adapter + type mapping tự bảo trì; được streaming tool UI, attachments, generative UI miễn phí về sau.
- Gate: chạy song song 2 protocol sau flag `AI_PROTOCOL=v2` cho đến khi FE chuyển xong.

### M2 — Resume sau approval (P0, ~3–5 ngày, BE)
Hoàn thiện HITL theo semantics LangGraph: sau `POST /api/ai/approvals/{id}/execution`, **chạy tiếp agent loop** với tool result + history, stream tiếp vào cùng thread — thay vì dừng và chờ user gõ lại.
- `resume.go`: sau khi execute approved tool, gọi lại `runAgentStream` với messages đã có tool message.
- Cần cẩn thận: `maxSteps` đếm riêng cho lượt resume; run status `WAITING_APPROVAL → RUNNING`.
- Gate: kịch bản test end-to-end "yêu cầu tạo giao dịch → approve → agent tự tổng kết không cần user gõ tiếp".

### M3 — Observability cho agent (P1, ~3–4 ngày, BE + infra)
- Bước 1 (không thêm service): emit OpenTelemetry GenAI semantic conventions (span per run, per LLM call, per tool) — sẵn sàng nếu đã có OTel collector; nếu chưa, thêm trước.
- Bước 2 (tùy chọn): Langfuse self-host trên k3s nếu muốn UI trace + dataset eval — cân nhắc sau khi dùng OTel thấy thiếu gì.
- Lợi ích: debug "run FAILED vì gì", đo token/chi phí per tenant (bổ trợ cho analytics của AI Gateway ở §3).

### M4 — Code-mode tối ưu kiểu Cloudflare (P2, khi số tool > ~15, BE)
Bạn **đã có** code-mode; đây là tối ưu theo bài [Cloudflare Code Mode](https://blog.cloudflare.com/code-mode-mcp/):
1. Sinh TypeScript `.d.ts` cho `arda.*` SDK từ tool Definitions (codegen vào `proto-generate` pipeline) — model đọc types ~1k tokens thay vì mô tả từng tool.
2. Raw tool results **ở lại trong sandbox**: hiện `compactToolFeedback` đẩy cả `data` về model. Đổi thành: sandbox giữ kết quả trong biến/scratch state, model chỉ thấy `summary` + path để query tiếp nếu cần (pattern Anthropic "filesystem làm context" — thay `boundContent` cắt cụt 8KiB).
3. MCP exposure: chỉ khi có nhu cầu client ngoài (IDE, Claude) — giữ registry hiện tại làm source of truth, MCP là một adapter read-only.

### Không làm (chủ động loại)
- ❌ Assistant sang Cloudflare Workers / Agents SDK — phá nguyên tắc §1.
- ❌ Vectorize/AI Search CF — pgvector đủ, dữ liệu giữ trong cluster.
- ❌ Kéo agent framework (LangChainGo/Eino) vào — tự viết đã gọn; chỉ đọc code tham khảo.
- ❌ RAG trước khi có use case sản phẩm rõ (§4.4).

## 6. Thứ tự thực thi đề xuất

```text
Tuần 1-2:  §3 AI Gateway (rủi ro thấp, giải đo latency + chi phí + DLP ngay)
Tuần 2-3:  M1 protocol (song song được nếu Gateway xong sớm)
Tuần 3-4:  M2 resume HITL
Tuần 4-5:  M3 observability
Sau đó:    M4 code-mode tối ưu + §4 RAG (khi sản phẩm yêu cầu)
```

Mỗi mốc kết thúc bằng: green CI (`check-*.mjs` + `go test`), 1 demo end-to-end ghi lại, cập nhật `AGENTS.md` nếu invariant thay đổi.

## 7. Quyết định (ĐÃ CHỐT ĐỦ — 2026-08-29)

1. **Data-flow — ĐÃ CHỐT:** luồng model qua AI Gateway CF; thiết kế switchable §3.5 (AI Gateway ↔ LiteLLM ↔ direct chỉ bằng env + allowlist).
2. **Embedding — ĐÃ CHỐT:** `@cf/qwen/qwen3-embedding-0.6b` trên Workers AI ngay bây giờ; thiết kế `Embedder` hai provider (§4.2, §4.5) để self-host vLLM/Ollama sau này chỉ là đổi env.
3. **Thứ tự milestone — ĐÃ CHỐT: M1 (protocol) trước, M2 (resume) sau.**
4. **SSRF allowlist — ĐÃ GIẢI QUYẾT trong §3.5:** `AI_MODEL_BASE_URL_ALLOWLIST` là cơ chế, mặc định = domain gateway CF.
5. **Sở hữu & xoay key — ĐÃ CHỐT, xem §3.6.**

### 3.6. Sở hữu và xoay key (ĐÃ CHỐT)

```text
CF Dashboard (admin CF account)      ── giữ provider key THẬT (opencode/zen, OpenAI, Workers AI token)
                                        + cấu hình DLP/fallback/cache. Xoay: quarterly + khi nghi ngờ leak.
arda-app-secrets (k8s, arda-infra)   ── chỉ giữ: AI_MODEL_API_KEY (gateway token),
                                        AI_EMBEDDING_API_TOKEN (CF API token scope: Workers AI run).
ai_tenant_settings                   ── key tenant = key scope-gateway (LiteLLM gọi là virtual key).
```

Nguyên tắc:
1. **Không một workload nào trong cluster giữ provider key gốc** — mất secret k8s không lộ được key ra ngoài.
2. Xoay key ở gateway không cần deploy lại ai-service (ai-service chỉ thấy gateway token, vốn tự nó xoay riêng).
3. Runbook xoay nằm ở `arda-infra/docs/` (viết khi triển khai §3.4): tạo key mới ở provider → cập nhật gateway → test `/api/ai/settings/test` → thu hồi key cũ.
4. Endpoint `/api/ai/settings/test` hiện có chính là công cụ verify sau mỗi lần xoay — thêm vào runbook.
