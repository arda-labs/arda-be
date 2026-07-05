# API Errors

Workspace contract for HTTP error responses across Arda backend services and MFE clients.

**Related:** [http-api.md](http-api.md) (list success shape, `X-Request-Id` headers), [i18n.md](i18n.md) (FE translation of `code`).

## JSON shape (domain services — target standard)

Services using `arda-errors` respond with:

```json
{
  "error": {
    "code": "validation.invalid_input",
    "message": "Request is invalid",
    "fields": { "email": "validation.required" },
    "request_id": "optional-correlation-id"
  }
}
```

Go types: `libs/go/arda-errors` — `ardaerrors.Error`, `ardaerrors.Response`.

## Common codes

| Code | Typical HTTP | Meaning |
| --- | --- | --- |
| `auth.error.unauthorized` | 401 | Missing or invalid auth |
| `auth.error.forbidden` | 403 | Authenticated but not allowed |
| `common.error.not_found` | 404 | Resource missing |
| `common.error.conflict` | 409 | Duplicate or state conflict |
| `validation.invalid_json` | 400 | Body not JSON |
| `validation.invalid_input` | 400 | General validation failure |
| `validation.required` | 400 | Required field (often in `fields`) |
| `common.error.internal` | 500 | Unexpected server error |
| `iam.user.not_found` | 404 | IAM-specific (extend per domain) |

Add domain codes as `\<service\>.\<entity\>.\<reason>` (e.g. `iam.superadmin.last_active`).

`ardaerrors.CodeForStatus(httpStatus)` maps status → default code when only status is known.

## Handler patterns (Go)

**Preferred (iam-service):**

```go
respondErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "email required")
respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeForbidden, "")
```

**Preferred (platform-service):** return `*ardaerrors.Error` from service layer; `writeResult` encodes it.

**Legacy (hrm-service, auth-gateway edge):** `{"error":"plain message"}` — align new code with `arda-errors`; do not copy legacy shape into new services.

## Field validation

- Use `err.WithField("fieldName", "validation.required")` for form-level errors.
- HTTP 400 with `fields` map — frontend maps to RHF `setError("fieldName", { message })`.

## Frontend mapping

1. `@workspace/core/http/api-client` parses error body on non-2xx.
2. `ApiErrorLike` in `@workspace/i18n` carries `code`, `message`, `status`, `fields`.
3. `translateApiError(error)` — looks up `code` in i18n resources, falls back to `message`.
4. Mutations: `notify.error("Action failed", translateApiError(error))`.
5. Forms: map `error.fields` to `form.setError` when present.

```tsx
catch (error) {
  const apiErr = toApiError(error)
  if (apiErr instanceof ApiErrorLike && apiErr.fields) {
    for (const [field, code] of Object.entries(apiErr.fields)) {
      form.setError(field as keyof Values, { message: translateApiError({ code }) })
    }
    return
  }
  notify.error("Save failed", translateApiError(error))
}
```

## Consistency roadmap

| Layer | Current | Target |
| --- | --- | --- |
| iam-service, platform-service | `arda-errors` envelope | Keep |
| hrm-service, newer CRUD | Simple `{"error":"..."}` | Migrate to `arda-errors` |
| auth-gateway ForwardAuth | Deny without JSON contract | Edge-only; not for domain APIs |

Skill: `.agents/skills/arda-api-errors` for implementation checklist.
