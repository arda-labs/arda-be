# HTTP API Contract

Contract thống nhất cho REST JSON qua **auth-gateway** → domain services. Liên quan:

- Lỗi: [api-errors.md](api-errors.md)
- i18n FE: [i18n.md](i18n.md)
- gRPC paging proto: `arda-be/proto/arda/common/v1/common.proto` (`PageRequest` / `PageResponse`)
- Skill: `arda-api-errors`, `arda-i18n`, `arda-backend-service`

---

## 1. Nguyên tắc

1. **JSON ổn định** — FE không parse theo từng service; một shape list, một shape error.
2. **Mã lỗi là contract** — `error.code` và `fields.*` là stable key; `message` chỉ fallback/debug.
3. **i18n ở FE** — BE không bắt buộc dịch user message; FE gửi `Accept-Language`, hiển thị qua `translateApiError(code)`.
4. **Correlation** — mọi response có `request_id`; `trace_id` khi OTel bật (header + optional body).
5. **Server-driven list** — paginate/filter/sort trên BE; FE đồng bộ query URL (nuqs) ↔ query API.

---

## 2. Request

### Headers (chuẩn)

| Header | Ai gửi | Mục đích |
| --- | --- | --- |
| `Accept-Language` | MFE (`api-client`) | `vi-VN` / `en-US` — audit, email template sau này |
| `Content-Type: application/json` | MFE (body JSON) | |
| `X-Request-Id` | Gateway hoặc MFE | UUID; gateway **generate nếu thiếu**, forward xuống service |
| `X-User-Id`, `X-Tenant-Id`, … | auth-gateway | Xem [auth-user-context.md](auth-user-context.md) |

Gateway/service **echo** `X-Request-Id` trên response. Khi có OpenTelemetry: thêm `X-Trace-Id` (W3C `traceparent` hoặc hex trace id).

### List query params (chuẩn mới)

| Param | Kiểu | Mặc định | Ghi chú |
| --- | --- | --- | --- |
| `page` | int | `1` | 1-based |
| `per_page` | int | `20` | Max `100` (service có thể giới hạn thấp hơn) |
| `sort` | string | — | Tên field DB/API (`name`, `created_at`) |
| `order` | `asc` \| `desc` | `asc` | |
| `q` | string | — | Full-text / search chung (tùy resource) |
| `*` | | | Filter theo field: `status`, `is_active`, `tenant_id`, … |

**Đồng bộ FE (nuqs):** URL dùng `page`, `perPage` → map sang `per_page` khi gọi API. Sort: map `sort` JSON nuqs → `sort` + `order` query (hoặc giữ JSON một param — chọn một, ưu tiên `sort` + `order` cho BE đơn giản).

**Legacy (removed phase 3):** ~~IAM `size`, `search`, `sortField`, `sortOrder`~~ — clients must use `per_page`, `q`, `sort`, `order`.

---

## 3. Response — thành công

### 3.1. List paginated (chuẩn)

```json
{
  "items": [
    { "id": "…", "code": "ORG01", "name": "Chi nhánh HN" }
  ],
  "page": 1,
  "per_page": 20,
  "total": 1234
}
```

- `items` — luôn array (rỗng `[]` nếu không có).
- `total` — tổng bản ghi **sau filter**, trước paginate.
- **Không** bọc thêm `data` wrapper cho list (giữ payload phẳng, dễ TanStack Query).
- **Không** dùng tên resource làm key (`users`, `organizations`) — dùng `items` thống nhất.

FE:

```ts
type ListResponse<T> = {
  items: T[]
  page: number
  per_page: number
  total: number
}
// pageCount = Math.ceil(total / per_page)
```

### 3.2. Single resource

Giữ **object phẳng** (đang dùng rộng rãi):

```json
{
  "id": "uuid",
  "code": "ORG01",
  "name": "…",
  "created_at": "2026-07-04T14:00:00Z"
}
```

Create/update: trả object vừa lưu, HTTP `201` (create) / `200` (update). Field naming: **snake_case** trong JSON mới (platform, IAM admin phase 4); camelCase legacy cho `UserContext` / BFF session — không trộn trong resource mới.

### 3.3. Action / void

```json
{ "ok": true }
```

hoặc `204 No Content` khi không cần body.

### 3.4. Metadata (`request_id`, thời gian, trace)

**Ưu tiên response headers** (không phá parser JSON hiện tại):

```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Request-Id: 7c9e6679-7425-40de-944b-e07fc1f90ae7
X-Trace-Id: 4bf92f3577b34da6a3ce929d0e0e4736
Date: Sat, 04 Jul 2026 14:00:00 GMT
```

| Field | Vị trí | Bắt buộc | Ghi chú |
| --- | --- | --- | --- |
| `request_id` | Header `X-Request-Id` | Có | Correlation support, audit |
| `trace_id` | Header `X-Trace-Id` | Khi OTel | Debug distributed trace |
| Thời gian | Header `Date` | HTTP chuẩn | |

**Optional (phase 2)** — `meta` trong body cho tooling/debug:

```json
{
  "meta": {
    "request_id": "7c9e6679-…",
    "trace_id": "4bf92f3577-…",
    "timestamp": "2026-07-04T14:00:00.000Z"
  },
  "items": [],
  "page": 1,
  "per_page": 20,
  "total": 0
}
```

Chỉ thêm khi có helper Go chung (`arda-http` / `writeList`) — tránh từng handler tự chế.

Lỗi: `error.request_id` trong body (đã có `arda-errors`) **và** header `X-Request-Id` — cùng giá trị.

---

## 4. Response — lỗi

Chuẩn `arda-errors` (chi tiết [api-errors.md](api-errors.md)):

```json
{
  "error": {
    "code": "validation.invalid_input",
    "message": "Request is invalid",
    "fields": {
      "code": "validation.required",
      "name": "validation.required"
    },
    "request_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7"
  }
}
```

| Quy tắc | |
| --- | --- |
| `code` | Stable, dịch ở FE — `domain.entity.reason` hoặc `validation.*` |
| `message` | English ngắn, **không** dùng làm UI chính |
| `fields` | Map field → **mã lỗi** (không phải câu tiếng Việt) |
| HTTP status | 400 validation, 401/403 auth, 404, 409, 500 |

**Legacy cấm cho API mới:** `{"error":"plain string"}`, `http.Error` text, key list lệch (`users` không `total`).

---

## 5. i18n end-to-end

```text
┌──────── FE ────────┐     Accept-Language: vi-VN
│ translateApiError  │◄──── error.code + fields.*
│ t("…") UI strings  │
└────────────────────┘
         ▲
         │ { error: { code, message, fields, request_id } }
┌──────── BE ────────┐
│ arda-errors codes  │  message = English fallback / log
│ không dịch UI      │
└────────────────────┘
```

1. **UI label** → `packages/i18n` keys (`vi-VN` + `en-US`).
2. **API business error** → `platform.organization.code_conflict` trong `arda-errors` + JSON locale mirror.
3. **Field validation** → `fields.email = "validation.email_invalid"` → FE `translateApiError({ code })` per field.
4. **Không** hiển thị `error.message` thô làm text chính (trừ dev / copy request_id).

Map code → JSON: quy tắc `normalizeKey` trong `@workspace/i18n` (`common.error.internal` → `common:error.internal`).

---

## 6. Go implementation (target)

Shared library: **`libs/go/arda-http`**

```go
import ardahttp "github.com/arda-labs/arda/libs/go/arda-http"

listRequest, err := ardahttp.ParseListRequest(r.URL.Query(), listSpec)
ardahttp.WriteList(w, r, listRequest.Page, listRequest.PerPage, total, items)
ardahttp.WriteErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "…")
```

| API | Mục đích |
| --- | --- |
| `ParseListRequest` / `ListSpec` | Validate pagination, sort, view và typed filters theo contract từng resource |
| `ParseListQuery` | Parser legacy; không dùng cho endpoint list mới |
| `WriteList` / `NewListResponse` | Body list chuẩn |
| `WriteJSON` / `WriteAppError` / `WriteErrorCode` | JSON + `X-Request-Id` |
| `RequestID` | Lấy/generate correlation id |

Handler pattern: parse query → service/repo → `WriteList` hoặc `WriteAppError`.

gRPC nội bộ: `arda.common.v1.PageRequest` / `PageResponse` — map `size` ↔ `per_page` ở HTTP boundary.

---

## 7. FE implementation (target)

Shared package: **`@workspace/api/list`**

```ts
import {
  buildListSearchParams,
  listPageCount,
  sortToApiParams,
  type ListResponse,
} from "@workspace/api/list"

const data = await api.get<ListResponse<Organization>>(
  `/api/platform/organizations?${buildListSearchParams({ page, perPage, q, sort, order })}`
)
// pageCount = listPageCount(data.total, perPage)
```

`@workspace/api/client` gửi `Accept-Language` + `X-Request-Id` mỗi request.

### 7.1 Server list state

Các trang list không tự fetch bằng cặp `useEffect` + `useCallback`. Dùng
`useServerList` từ `@workspace/admin-list/server-list`; TanStack Query quản lý
cache, request dedupe, cancellation và giữ dữ liệu trang trước khi đổi page.
`QueryProvider` thuộc `@workspace/query/provider`; phần table/list orchestration
thuộc `@workspace/admin-list`, không nằm trong design system `@workspace/ui`.

```tsx
const customers = useServerDataTable({
  ...customerListDefinition,
  columns,
  queryFn: (query, { signal }) => crmApi.listCustomers(query, { signal }),
})
```

Table filter và advanced-search dùng cùng một schema URL → API:

```tsx
const customerListDefinition = defineServerList({
  queryKey: ["crm", "customers", "list"],
  queryConfig: {
    defaultPageSize: 20,
    sortableColumns: ["code", "name", "created_at"],
    filters: [
      { urlKey: "q", apiKey: "q", mode: "text" },
      { urlKey: "name", apiKey: "q", mode: "text" },
      { urlKey: "is_active", mode: "single", allowedValues: ["true", "false"] },
      { urlKey: "status", mode: "multi", allowedValues: ["new", "approved"] },
    ],
  },
})

// Advanced form submit: ghi vào cùng URL state, tự reset page=1.
setSearchParams((current) =>
  applyServerListFilters(current, formValues, customerListDefinition.queryConfig)
)
```

Backend dùng parser typed từ `arda-http`, không tự so sánh chuỗi ở handler:

```go
var customerListSpec = ardahttp.ListSpec{
    DefaultPerPage: 20,
    MaxPerPage:     100,
    SortFields:     []string{"code", "name", "created_at"},
    Filters: map[string]ardahttp.QueryFilterSpec{
        "is_active": ardahttp.BoolFilter(),
        "status":    ardahttp.CSVFilter(10, "new", "approved"),
    },
}

request, err := ardahttp.ParseListRequest(r.URL.Query(), customerListSpec)
isActive := request.Bool("is_active")
statuses := request.Strings("status")
```

Giá trị filter sai contract trả `400 validation.invalid_input`; không âm thầm
bỏ qua rồi trả dữ liệu không được lọc.

Quy ước:

1. Mỗi resource khai báo một `defineServerList`; query key bắt đầu bằng
   `[domain, resource, variant]`, variant thường là `list`, `tree`, `options`
   hoặc `detail`.
2. Query key dùng query đã serialize ổn định, không dùng object identity làm
   dependency fetch.
3. `list`, `tree` và `options` là cache độc lập. Chỉ enable khi UI thực sự cần;
   không tải lại reference/options khi đổi page của bảng.
4. Search trong table và search nâng cao phải khai báo trong cùng
   `ServerListQueryConfig`. Search nhanh có debounce; advanced search tách
   `draftFilters` và `appliedFilters`. Chỉ applied filters đi vào URL/query key
   và `applyServerListFilters` reset page về 1.
5. API client nhận `AbortSignal`; request cũ phải được hủy khi query đổi.
6. Create/update/delete invalidate prefix `[domain, resource]`; chỉ query đang
   active tự refetch, query inactive được đánh dấu stale.
7. `QueryClientProvider` đặt ở boundary của MFE. Khi chia cache toàn shell, cả
   provider và `@tanstack/react-query` phải là Module Federation singleton.
8. Page server-side dùng `useServerDataTable`, không ghép riêng
   `useServerListQuery` và `useDataTable`; tránh hai parser URL lệch nhau.

Page vẫn sở hữu columns, form và nghiệp vụ. Không đưa CRUD/domain logic vào một
generic table component.

---

## 8. Trạng thái hiện tại vs target

| Topic | Hiện tại | Target |
| --- | --- | --- |
| List shape | **Tất cả list endpoints ✅** `{ items, page, per_page, total }` | via `arda-http.WriteList` |
| Query | **Chuẩn `per_page`, `q`, `sort`, `order` ✅** | Không alias legacy |
| Error | **Tất cả domain services ✅** | `arda-errors` |
| `request_id` | Body error + **gateway echo header ✅** | Header + body error |
| `trace_id` | **Gateway echo `X-Trace-Id` ✅** (từ `traceparent` hoặc header) | Header + optional body |
| FE i18n | `translateApiError` sẵn | Map đủ codes mới |
| Meta timestamp | **Optional `meta` in JSON body ✅** (`request_id`, `trace_id`, `timestamp`) | Header `Date`; via `arda-http.WriteJSON` |

---

## 9. Thứ tự migrate

1. **Doc + proto** — contract này + `PageResponse` align naming (`per_page` alias doc).
2. **platform organizations** — list paginated BE + FE (pilot end-to-end).
3. **Shared Go `WriteList` / `WriteError`** — auth-gateway echo `X-Request-Id`.
4. **IAM list endpoints** — `items` + `per_page` (giữ alias `size` deprecated một đợt nếu cần).
6. **FE** — `ListResponse<T>`, `buildListSearchParams`, pages đọc `.items`.

Phase 4 (done): IAM admin single-resource JSON snake_case; FE maps to camelCase types in `api.ts`. Phase 5 (done): `meta` in JSON body; IAM session/admin session endpoints snake_case. Phase 6 (done): internal IAM session API snake_case + arda-errors; auth-gateway iamclient aligned; MFA verify `user_id`. Phase 7 (done): internal identity resolve request snake_case; `iam` i18n namespace. **Browser boundary:** `UserContext`, BFF session, `/api/auth/me` stay camelCase.

---

## 10. Ví dụ đầy đủ

**Request**

```http
GET /api/platform/organizations?page=2&per_page=10&sort=name&order=asc&q=hanoi&is_active=true
Accept-Language: vi-VN
X-Request-Id: 7c9e6679-7425-40de-944b-e07fc1f90ae7
```

**Response 200**

```http
X-Request-Id: 7c9e6679-7425-40de-944b-e07fc1f90ae7
Content-Type: application/json
```

```json
{
  "items": [
    {
      "id": "a1b2c3d4-…",
      "code": "HN01",
      "name": "Chi nhánh Hà Nội",
      "is_active": true,
      "created_at": "2026-06-01T08:00:00Z"
    }
  ],
  "page": 2,
  "per_page": 10,
  "total": 47
}
```

**Response 400**

```json
{
  "error": {
    "code": "validation.invalid_input",
    "message": "Request is invalid",
    "fields": { "per_page": "validation.range" },
    "request_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7"
  }
}
```

FE: `notify.error(t("platform.organizations.load_failed"), translateApiError(err))` — user thấy tiếng Việt từ key `validation.range`, support copy `request_id` khi báo lỗi.
