# API Errors & Problem Details Standard

Workspace contract for HTTP error responses across Arda Go backend microservices and MFE clients.

**Related:** [http-api.md](http-api.md) (list success shape, `X-Request-Id`, `X-Trace-Id` headers), [i18n.md](i18n.md) (FE translation of `code`), [problem details catalog](../problems/README.md).

---

## 1. Canonical Error Shapes

### RFC-7807 Problem Details (`ardahttp.WriteProblem`)
Endpoint sử dụng `Content-Type: application/problem+json` với cấu trúc flat, đính kèm `request_id` và W3C `trace_id`:

```json
{
  "type": "https://docs.arda.io.vn/problems/crm.customer.conflict",
  "title": "Conflict",
  "status": 409,
  "code": "crm.customer.conflict",
  "message": "customer already has a pending amendment",
  "errors": [
    { "field": "tax_code", "code": "validation.required", "message": "validation.required" }
  ],
  "request_id": "83b1293a-8742-4916-b864-16a782163b2f",
  "trace_id": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
}
```

### Application Error Envelope (`ardahttp.WriteAppError`)
```json
{
  "error": {
    "code": "validation.invalid_input",
    "message": "Request is invalid",
    "fields": { "email": "validation.required" },
    "request_id": "83b1293a-8742-4916-b864-16a782163b2f"
  }
}
```

Go shared packages:
* `libs/go/arda-errors` (`*ardaerrors.Error`, `ardaerrors.Response`)
* `libs/go/arda-http` (`ardahttp.WriteProblem`, `ardahttp.WriteAppError`, `ardahttp.WriteSuccess`)

---

## 2. Common Canonical Codes

| Code | Typical HTTP | Meaning |
| :--- | :--- | :--- |
| `auth.error.unauthorized` | 401 | Missing or invalid auth token/session |
| `auth.error.forbidden` | 403 | Authenticated but insufficient permission |
| `tenant.error.scope_required` | 403 | Missing active tenant or organization scope (`ardaerrors.CodeTenantScopeRequired`) |
| `tenant.error.migration_required` | 403 | Tenant migration needed |
| `common.error.not_found` | 404 | Resource missing |
| `common.error.conflict` | 409 | Duplicate entry or state machine conflict |
| `validation.invalid_json` | 400 | Request body is not valid JSON (`ardaerrors.CodeInvalidJSON`) |
| `validation.invalid_input` | 400 | General validation failure |
| `validation.required` | 400 | Required field missing |
| `common.error.internal` | 500 | Unexpected server panic/error |
| `common.error.bad_gateway` | 502 | Upstream service failure (e.g. gRPC client failure) |

---

## 3. Go Handler Implementation Standard

```go
// 1. Lỗi validation / bad request:
writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid request body")

// 2. Lỗi thiếu tenant / org scope:
writeErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeTenantScopeRequired, "tenant and organization are required")

// 3. Lỗi Service layer / SQL:
writeServiceError(w, r, err) // Tự động map sql.ErrNoRows -> 404, ardaerrors.Error -> code/status tương ứng

// 4. Thành công:
ardahttp.WriteSuccess(w, r, http.StatusOK, result)
```

---

## 4. Frontend Integration Contract

1. `@workspace/api/client` tự động bắt mọi HTTP non-2xx, trích xuất `code`, `message`, `fields`, `request_id`.
2. `@workspace/ui/feedback/notify` kết hợp `@workspace/i18n`:
   * `notify.saveFailed(err)` hoặc `notify.apiError(title, err)` tự động dịch `code` sang ngôn ngữ hiện tại của người dùng.
   * `error.fields` được tự động map vào React Hook Form `setError("fieldName", { message })`.
