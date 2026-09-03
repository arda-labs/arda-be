# Arda AI & RAG Architecture (Single Source of Truth)

Status: **Hoàn thiện & Hợp nhất toàn bộ về Go thuần (Go-native)**
Ngày cập nhật: **2026-09-03**
Sở hữu: `arda-be/apps/ai-service`

---

## 1. Tổng quan kiến trúc

Toàn bộ năng lực AI & RAG trong hệ sinh thái Arda được hợp nhất duy nhất tại một microservice viết bằng **Go**: **`ai-service`** (`arda-be/apps/ai-service`).

Không còn runtime Python, không còn adapter Node.js, không còn network hop trung gian.

```text
[ React MFE (apps/shell + apps/platform) ]
                  │
                  ▼ (Cookie-based HTTP / SSE)
        [ auth-gateway (BFF) ]
                  │ (Workload identity HMAC-SHA256)
                  ▼
   ┌─────────────────────────────────────────────────────────────┐
   │                  ai-service (Go, Port 8098)                 │
   │                                                             │
   │  1. Agent Loop & AG-UI Protocol (/api/ai/agent)             │
   │  2. Conversations & HITL Approvals Engine                   │
   │  3. In-process RAG & Knowledge Engine:                      │
   │     - Chunker: Heading-based markdown splitting + overlap   │
   │     - Parser: Text, Markdown, CSV, JSON extractor           │
   │     - Embedder: OpenAI / Cloudflare Workers AI client       │
   │     - Hybrid Search: pgvector (<=>) + FTS (simple) + RRF    │
   │     - Background Ingestion Worker (Postgres SKIP LOCKED)    │
   │     - HTTP API Handlers (/api/rag/*)                        │
   └──────────────────────────────┬──────────────────────────────┘
                                  │
                                  ▼
                    [ PostgreSQL: Database `ai` ]
```

---

## 2. Các thành phần cốt lõi

### 2.1. Frontend & Giao thức Agent (AG-UI Protocol)
- Giao thức chuẩn: **AG-UI** (official assistant-ui runtime cho backend phi JS).
- Endpoint: `POST /api/ai/agent`.
- Truyền sự kiện qua Server-Sent Events (SSE): text stream, tool execution, interrupts (Human-in-the-loop) và resume.
- Phía MFE: gói `@workspace/ai` cung cấp `useAgUiRuntime` + `HttpAgent`.

### 2.2. Bộ máy tri thức & RAG nội bộ (`internal/knowledge`)
- **Tách Chunk (`chunker.go`)**: Cắt nhỏ văn bản theo Markdown Headings (`# `, `## `, `### `), áp dụng thuật toán sliding window với `chunk_size` (mặc định 512 từ) và `chunk_overlap` (mặc định 64 từ).
- **Trích xuất tài liệu (`parser.go`)**: Đọc tài liệu nhị phân hoặc văn bản tải lên thành Markdown có cấu trúc.
- **Tạo Vector Embedding (`embedder.go`)**: Gọi Cloudflare Workers AI hoặc OpenAI embedding endpoint (model `@cf/qwen/qwen3-embedding-0.6b` hoặc `text-embedding-3-small`, 1024 chiều).
- **Tìm kiếm kết hợp (Hybrid Search - `repository.go`)**:
  - **Nhánh Vector**: So khớp khoảng cách Cosine `c.embedding <=> $1::vector` có index HNSW.
  - **Nhánh FTS**: Truy vấn toàn văn `to_tsvector('simple', c.content) @@ plainto_tsquery('simple', $query)`.
  - **Hợp nhất điểm số (RRF Fusion)**: Công thức chuẩn $RRF\_Score = \sum \frac{1}{60 + rank}$.
- **Hàng đợi nhúng ngầm (Ingestion Worker - `service.go`)**: Goroutine chạy nền định kỳ quét bảng `ai_ingestion_jobs` với khóa `FOR UPDATE SKIP LOCKED`, tự động chia chunk và sinh vector embedding.

---

## 3. Ranh giới API & Phân quyền

Toàn bộ request từ bên ngoài đều đi qua `auth-gateway`:

| Endpoint | Quyền hạn yêu cầu | Mô tả |
|---|---|---|
| `POST /api/ai/agent` | `ai.assistant.use` | Khởi chạy Agent loop, stream AG-UI events |
| `GET /api/ai/conversations` | `ai.assistant.use` | Quản lý lịch sử hội thoại |
| `POST /api/ai/approvals/**` | `ai.approval.propose` / `ai.approval.execute` | Quản trị phê duyệt HITL |
| `POST /api/rag/query` | `ai.assistant.use` | Truy vấn tìm kiếm tri thức (Hybrid + Citations) |
| `POST /api/rag/feedback` | `ai.assistant.use` | Đánh giá độ hữu ích của kết quả RAG |
| `GET/POST /api/rag/sources` | `ai.knowledge.manage` | Danh mục nguồn tri thức |
| `POST /api/rag/sources/{id}/versions` | `ai.knowledge.manage` | Tạo phiên bản tri thức |
| `POST /api/rag/sources/{id}/versions/{vid}/review` | `ai.knowledge.manage` | Duyệt / Từ chối phiên bản tri thức |
| `POST /api/rag/sources/{id}/versions/{vid}/publish` | `ai.knowledge.manage` | Xuất bản phiên bản $\rightarrow$ kích hoạt Ingestion Job |
| `POST /api/rag/sources/preview-chunks` | `ai.knowledge.manage` | Xem trước Chunks từ Markdown text |
| `POST /api/rag/sources/parse-preview` | `ai.knowledge.manage` | Kéo thả file tải lên, parse và xem trước Chunks |

---

## 4. Cấu trúc Database (`database: ai`)

Tất cả bảng ứng dụng nằm trong schema `public`, quản lý tập trung bằng **Goose migrations** trong `apps/ai-service/migrations/`:

* `ai_conversations`: Quản lý phiên hội thoại của người dùng theo `tenant_id` và `actor_user_id`.
* `ai_messages`: Lưu lịch sử tin nhắn và chuỗi sự kiện.
* `ai_runs`: Quản lý trạng thái thực thi của từng lượt chat, token usage và model sử dụng.
* `ai_tool_executions`: Lịch sử gọi tool và dữ liệu đã redact nhạy cảm.
* `ai_approvals`: Trạng thái phê duyệt Human-In-The-Loop.
* `ai_knowledge_sources`: Danh mục nguồn tri thức (`BIGSERIAL id`, scope tenant/global, classification).
* `ai_knowledge_source_versions`: Các phiên bản nội dung của nguồn (DRAFT, APPROVED, PUBLISHED).
* `ai_knowledge_chunks`: Từng đoạn chunk kèm vector embedding (`vector(1024)` + index HNSW).
* `ai_ingestion_jobs`: Hàng đợi xử lý chunking và embedding nền.
* `ai_rag_runs`: Ghi nhận nhật ký truy vấn để đo độ trễ và số ứng viên tìm kiếm.
* `ai_rag_feedback`: Phản hồi chất lượng từ người dùng.
