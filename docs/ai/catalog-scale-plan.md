# WP5–WP9: Lộ trình scale Catalog & Capability Discovery

> **TRẠNG THÁI (2026-09-01): WP5 ĐÃ HIỆN THỰC.** Catalog-as-data đang chạy:
> `contracts/ai-internal/{iam,crm,finance}-v1.json` annotate `x-ai-tool` →
> `tools/catalog-gen` sinh `internal/catalog/generated.go` (commit, auditable)
> → generic executor `httptarget.go` thực thi qua signed delegated transport
> với response allowlist. CI: `scripts/check-ai-catalog.mjs` trong verify.yml
> (stale generated.go, ghost permission, sai domain/kind, route ngoài
> /internal/ai/*). Entries còn ở catalog Go tay: `iam.me`,
> `iam.listCapabilities`, `knowledge.search`, `crm.exportCustomer` (stub —
> CRM chưa có export backend; chuyển sang contract khi route + permission
> `crm.customer.export` tồn tại). WP6–WP9: chờ theo lộ trình dưới đây.
> Kế thừa `agent-evolution-roadmap.md` (WP0–WP4 đã xong) và **hiện thực hóa**
> `sdk-catalog-design.md` §3 (OpenAPI `x-ai-tool` + catalog-gen) và §7 (CI
> consistency checks) — hai phần đã thiết kế sẵn nhưng chưa làm.
>
> Bối cảnh: 2026-09-01 `arda.iam.listUsers` mới chạy được ở production sau khi
> thiếu env `IAM_SERVICE_URL` trong `arda-infra` → tool bị skip đăng ký im lặng
> (`catalog/iam.go` early-return khi `BaseURL == ""`). Bài học: catalog là khớp
> nối sống giữa ai-service và *mọi* service khác; khi số service/API tăng,
> quy trình "sửa Go trong ai-service cho mỗi API mới" sẽ vỡ.
>
> Tham chiếu industry (2026-09): Anthropic *Code Execution with MCP* (tool
> search + filesystem-as-catalog: 150k → 2k tokens, ~98,7%), AWS Bedrock
> AgentCore Gateway (OpenAPI → MCP tools tự động, semantic tool selection,
> ingress/egress auth một cửa), OpenAI tool search / deferred loading. Kiến
> trúc Arda đã đúng hướng (code-mode sandbox + meta-tool); kế hoạch này đóng
> khoảng trống còn lại.

## 0. Ý tưởng chủ đạo (một câu)

**Service tự khai báo AI surface của mình ngay trong OpenAPI contract mà nó
đã có; pipeline sinh catalog tự động; model chỉ nhận mục lục mỏng + tra cứu
theo nhu cầu; authorization vẫn enforce tập trung tại dispatcher + service.**

Hệ quả: ai-service rời khỏi critical path của mọi feature — team domain thêm
tool cho AI bằng cách commit annotation vào contract của chính mình, không sửa
code ai-service, không deploy ai-service.

## 1. Nguyên tắc bất biến (kế thừa + bổ sung)

1–3 giữ nguyên từ `agent-evolution-roadmap.md` §1 (authorization tập trung,
self-hosted k3s, Go-native boundary). Bổ sung bất biến riêng của catalog:

4. **Catalog là compile-time artifact** — `generated.go` commit vào repo,
   auditable qua git diff, KHÔNG hot-reload / runtime fetch OpenAPI
   (đã chốt trong `sdk-catalog-design.md` §3.3).
5. **Progressive disclosure** — model chỉ nhận mục lục; chi tiết pull qua
   `search()`/`listCapabilities`; khi catalog vượt ngưỡng token thì giảm độ
   chi tiết của mục lục, không tăng prompt vô hạn.
6. **Liệt kê không phải ủy quyền** — `RequiredPermissions` trên entry chỉ là
   filter để đỡ lãng phí bước gọi; authorization thật luôn ở dispatcher
   (`entry.CheckPermissions`) và service đích (re-validate delegated scope).

## 2. Hiện trạng (2026-09-01) — điểm xuất phát

| Thành phần | Trạng thái | Vị trí |
|---|---|---|
| Catalog entries (Go code tay) | ✅ 7 entry: `iam.me`, `iam.listCapabilities`, `iam.listUsers`, `crm.getCustomer`, `crm.exportCustomer`, `finance.getAccount`, `knowledge.search` | `internal/catalog/*.go` |
| Meta-tools search/execute/readResult | ✅ | `internal/tools/` |
| BM25 search index | ✅ (đã vượt thiết kế §4 — không chờ vector) | `internal/catalog/index.go` |
| TS `.d.ts` sinh từ catalog, inject 1 lần | ✅ (M4 mảnh 1) | `internal/catalog/typescript.go` |
| Raw result ở sandbox + `readResult` | ✅ (M4 mảnh 2) | `internal/sandbox/` |
| Permission-filter khi liệt kê | ✅ `entry.CheckPermissions` | `internal/catalog/iam.go:170` |
| OpenAPI `x-ai-tool` annotations | ❌ chưa có | `contracts/openapi/` |
| CLI `catalog-gen` | ❌ chưa có | `tools/` (có `arda-cli` sẵn) |
| CI consistency check catalog ↔ OpenAPI ↔ policy | ❌ chưa có | `scripts/check-*.mjs` |
| Semantic re-ranking | ❌ (chủ ý — BM25 đủ ở quy mô này) | — |
| MCP exposure | ❌ (chờ client ngoài — M4 mảnh 3) | — |

## 3. Kiến trúc đích

```text
Domain team (crm/finance/hrm/...)                    ai-service
─────────────────────────────────                    ──────────────────────────────
OpenAPI spec + x-ai-tool annotation                  go run ./tools/catalog-gen
  contracts/openapi/crm-v1.json ────────────────►      │ đọc mọi annotated spec
  (team domain sở hữu, PR bình thường)                 ▼
                                                     internal/catalog/generated.go  (commit)
auth-gateway policy.yaml ◄── check-ai-catalog ──────► internal/catalog/manual.go (builtin/
  (mọi RequiredPermissions phải tồn tại)               orchestration phức tạp, ít)
                                                          │ RegisterCatalog() startup
                                                          ▼
                                                     Registry ──► GenerateTypeDefinitions
                                                              ──► BM25 index
                                                              ──► (sau: vector re-rank)
                                                          ▼
                                                     search() / listCapabilities / execute()
                                                     (sandbox Goja, delegated headers,
                                                      enforce permission tại thực thi)
```

## 4. Các work package

### WP5 — Catalog-as-Data: `x-ai-tool` + `catalog-gen` (P0, ~3–4 ngày, BE)

Hiện thực hóa đúng thiết kế `sdk-catalog-design.md` §3.1–§3.3:

1. **CLI `tools/catalog-gen`** (Go, có thể là subcommand của `arda-cli`):
   đọc các spec `contracts/openapi/*-v*.json`, với mỗi operation có
   `x-ai-tool` → sinh `CatalogEntry` (param schema → TS type, `summary` →
   JSDoc dòng đầu, endpoint/service → executor generic).
2. **Executor generic thay closure Go tay**: một dispatcher duy nhất nhận
   `{service, method, pathTemplate}` từ annotation + JSON-Schema validate args
   (schema sinh từ OpenAPI), dựng delegated headers như `scopeToMetadata`.
   Xóa ~40 dòng Go closure cho mỗi method thường.
3. **Mapping mặc định kind/risk theo HTTP method** (ghi đè được bằng
   annotation): `GET` → `read`/`low`; `POST/PUT/PATCH/DELETE` →
   `confirm`/`medium` (mutation luôn đi HITL approval). Mọi ngoại lệ phải ghi
   rõ trong annotation — có lý do, có audit.
4. **Migration thứ tự**: `iam.listUsers` → `crm.getCustomer` →
   `crm.exportCustomer` → `finance.getAccount`. Giữ `manual.go` cho
   `knowledge.search` và các orchestration multi-endpoint (đúng Source B).
5. **CI `scripts/check-ai-catalog.mjs`** (mới, chạy trong `check-*.mjs` suite):
   - regenerate + `git diff --exit-code` (generated.go không stale);
   - mọi `sdkPath` phải map về 1 route OpenAPI thật (chống rot kiểu
     `IAM_SERVICE_URL` — lỗi cấu hình phát hiện ở CI, không phải lúc model
     gọi 404);
   - mọi `RequiredPermissions` phải tồn tại trong `auth-gateway/configs/policy.yaml`
     (hoặc permission registry tương đương);
   - `kind: read` không được trỏ vào method ghi.

**Gate WP5:** xóa `crm.getCustomer`/`crm.exportCustomer` khỏi Go registry,
chỉ còn annotation trong spec → `generated.go` sinh lại, `go test ./...` xanh,
agent vẫn trả lời đúng kịch bản demo cũ. Từ đây "thêm tool AI mới" =
commit annotation, **không đụng ai-service**.

### WP6 — Domain onboarding runbook (P0, ~1 ngày, docs + script)

Đi cùng WP5, biến trải nghiệm WP5 thành quy trình lặp được:

1. Checklist 1 trang: annotation `x-ai-tool` → `policy.yaml` (route cho
   auth-gateway + permission cho tool) → route `/internal/ai/*` trên service
   → middleware `internalAIService` tương đương → env `*_SERVICE_URL` ở
   Deployment service đó **và ở ai-service** (bài học 2026-09-01) → test.
2. Script `scripts/check-ai-catalog.mjs` gộp luôn việc cảnh báo
   "`x-ai-tool` khai báo nhưng service env chưa wire" bằng cách đối chiếu
   `arda-infra` manifest (env `*_SERVICE_URL`) — optional, làm khi kịp.
3. **Gate WP6:** onboarding thử nghiệm 1 domain mới (khuyến nghị `hrm`)
   hoàn toàn theo runbook bởi người chưa từng đụng ai-service.

### WP7 — Context budget: mục lục nén (P1, ~2 ngày, BE — **trigger-based**)

**Trigger:** TypeDefs sinh ra vượt **~4–6k tokens** (đo bằng M3 metrics —
đếm chars/4 của `ModelSDKTypes`, emit gauge `ai_sdk_types_tokens`).

- Đổi nội dung `sdkTypesMessage`: thay full dump bằng **bản đồ domain nén**
  (mỗi domain 1 dòng: tên, mục đích 1 câu, số method) + nhắc model dùng
  `search()`/`listCapabilities` để lấy signature chi tiết trước khi execute.
- Feature flag `AI_SDK_CONTEXT_MODE=full|compact` (mặc định full đến khi
  trigger) để A/B và rollback 1 dòng env.
- **Gate WP7:** eval set 20–30 câu tiếng Việt vàng (mỗi câu trỏ về đúng 1
  tool) — tỷ lệ chọn đúng tool ở chế độ compact không tụt quá 5% so với full;
  token/run giảm đo được.

### WP8 — Semantic re-ranking cho search (P2, ~2 ngày, BE — **trigger-based**)

**Trigger:** eval set cho thấy BM25 trượt trên paraphrase (vd. "khách nào
gặp rủi ro" không khớp keyword "customer risk") hoặc model phải search
nhiều vòng mới ra tool.

- Theo đúng dự phòng của `sdk-catalog-design.md` §4.1 ("Embeddings can be
  added as a re-ranking layer later without changing the API").
- Embed JSDoc+keywords lúc startup bằng interface `knowledge.Embedder` có
  sẵn (WorkersAI qwen3-embedding, đã chốt ở roadmap chính §4.2); cache theo
  content-hash entry, chỉ re-embed khi entry đổi.
- Hybrid: BM25 rank (hiện có) + cosine re-rank top-k. Tenant không liên quan
  — catalog là system-level, permission-filter vẫn chạy sau search.
- **Gate WP8:** eval set cải thiện rõ; thêm latency search < 300ms.

### WP9 — MCP exposure (P3, khi sự kiện xảy ra — M4 mảnh 3)

Chỉ làm khi xuất hiện client ngoài thật (IDE, Claude Desktop, agent của
khách): adapter read-only render Registry ra MCP `tools/list` + `tools/call`,
giữ Registry làm source of truth duy nhất. Không thiết kế trước khi có
consumer — tránh spec/view hoài.

## 5. Thứ tự thực thi

```text
Tuần 1:   WP5 catalog-gen + annotations + CI check   (P0, chặn mọi thứ phía sau)
Tuần 1-2: WP6 runbook + onboarding hrm thử nghiệm    (P0, song song được)
Theo trigger: WP7 mục lục nén (khi TypeDefs > 4-6k tokens)
Theo trigger: WP8 semantic re-rank (khi BM25 trượt trên eval set)
Theo sự kiện: WP9 MCP (khi có client ngoài)
```

Mỗi WP đóng theo đúng nghi thức roadmap chính: CI xanh (`check-*.mjs` +
`go test`), 1 demo end-to-end ghi lại, cập nhật trạng thái trong tài liệu này
và `README.md` index.

## 6. Đo lường thành công

| Chỉ số | Hiện tại | Mục tiêu |
|---|---|---|
| Time-to-AI-tool cho 1 API mới | sửa Go ai-service + deploy | commit annotation, không đụng ai-service |
| Token cơ sở / run (SDK types) | ~1k (7 entry) | phẳng theo thời gian (WP7) dù catalog lớn |
| Tỷ lệ run có `ai.tool_not_found` / `forbidden` | chưa đo | đo từ M3, giảm dần |
| Search precision (eval set vàng) | chưa đo | baseline WP5, cải thiện WP8 |
| Lỗi cấu hình kiểu env thiếu | phát hiện ở prod (2026-09-01) | phát hiện ở CI (`check-ai-catalog`) |

## 7. Chủ động KHÔNG làm

- ❌ **Hot-reload / runtime fetch catalog từ service** — phá tính predictable,
  mở thêm lớp failure; compile-time artifact đủ (chốt sẵn §3.3).
- ❌ **Vector-only search cho catalog** — BM25 đủ dưới ~200 entry; vector chỉ
  là re-rank khi có bằng chứng (WP8).
- ❌ **Tenant tự đăng ký tool** — AI surface là sản phẩm platform; tenant scope
  áp dụng trên *dữ liệu*, không phải trên *khả năng*.
- ❌ **Kéo agent framework / MCP SDK Go runtime mới** — giữ Go-native (§1.3).
- ❌ **Load test agent qua `arda-perf`** — script bắn thẳng production; chỉ
  chạy sau khi xác nhận riêng (AGENTS.md).

## 8. Rủi ro & phòng ngừa

| Rủi ro | Phòng ngừa |
|---|---|
| Annotation trôi lệch code thật (spec đổi, annotation quên) | `check-ai-catalog` regenerate + diff-exit-code mỗi CI |
| Executor generic mất kiểu an toàn của closure tay | JSON-Schema validate bắt buộc + unit test mỗi entry qua bảng test chung |
| Mục lục nén làm model chọn sai tool (WP7) | Eval set vàng + flag `full|compact` rollback ngay |
| Embedder ngoài (CF) chậm/lỗi lúc startup (WP8) | Embedding async + fallback về BM25 thuần khi chưa sẵn sàng |
| Catalog lớn dần làm `listCapabilities` mặc định (20/page) chậm | BM25 O(n) ổn đến ~200 entry; thêm prefix filter theo domain nếu vượt |
