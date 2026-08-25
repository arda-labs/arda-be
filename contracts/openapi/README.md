# Public OpenAPI contracts

This directory is the versioned source of truth for migrated public REST
operations. Each operation must define its request, response profile, problem
codes, authentication/scope policy, pagination limits, and credential
behavior. Domain adapters in the MFE may map generated wire types to view
models, but presentation code must not duplicate the wire contract.

The current pilots are `iam-v1.json` for the admin permissions
list/create/delete surface, `auth-v1.json` for the canonical browser session
user read, and `media-v1.json` for multipart upload and binary/redirect
download profiles. Internal media attachment is a versioned gRPC command,
not a browser REST operation. They intentionally describe only the
migrated operations; provider-native OAuth/Kratos, SSE, and internal gRPC
retain their protocol-specific contracts.

The lightweight check script verifies that the document is valid JSON,
OpenAPI 3.1, has operation IDs, declares the canonical success/problem
schemas, and does not omit cookie credentials from browser-facing operations.
Replace it with the approved OpenAPI linter/breaking-change tool when ADR-005
is accepted.
