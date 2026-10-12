# Public OpenAPI contracts

This directory is the versioned source of truth for migrated public REST
operations. Each operation must define its request, response profile, problem
codes, authentication/scope policy, pagination limits, and credential
behavior. Domain adapters in the MFE may map generated wire types to view
models, but presentation code must not duplicate the wire contract.

The current pilots are `iam-v1.json` for the admin permissions
list/create/delete surface, `auth-v1.json` for the canonical browser session
user read, `media-v1.json` for multipart upload and binary/redirect download
profiles, and `finance-v1.json` for document-number renumber requests and
maker-checker approval. Internal media attachment is a versioned gRPC command,
not a browser REST operation. They intentionally describe only the
migrated operations; provider-native OAuth/Kratos, SSE, and internal gRPC
retain their protocol-specific contracts.

The lightweight check script verifies that the document is valid JSON,
OpenAPI 3.1, has operation IDs, declares the canonical success/problem
schemas, and does not omit cookie credentials from browser-facing operations.
Replace it with the approved OpenAPI linter/breaking-change tool when ADR-005
is accepted.

## Generated documents

`deposit-v1.json` is generated from Go, not written by hand: its operations are
declared in `apps/deposit-service/internal/transport/http/openapi.go` and its
schemas come from the Go types through `libs/go/arda-openapi`. Regenerate it
with:

```bash
cd apps/deposit-service
ARDA_UPDATE_OPENAPI=1 go test ./internal/transport/http -run TestOpenAPIDocumentIsCommitted
```

`TestOpenAPIDocumentIsCommitted` fails when the committed file is stale and
`scripts/check-openapi-routes.mjs` fails when a public route is missing from
it. arda-mfe generates its wire types from every document here (ADR-005).
