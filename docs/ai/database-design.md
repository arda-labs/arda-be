# AI database design

Status: proposed design for review. This document authorizes no database
change until the approval gates in [rollout-plan.md](rollout-plan.md) pass.

## Ownership decision

Create a service-owned PostgreSQL database named `ai`, with an `ai` schema
inside it. The AI service owns migrations and the database role. This follows
Arda's existing rule that a service owns its schema and does not query another
service's tables.

The `ai` schema is a namespace boundary, not permission to share IAM/domain
tables. Store stable foreign IDs and resolve display data through contracts.

## Proposed tables

### `ai.conversations`

`id uuid`, `tenant_id uuid`, `actor_user_id uuid`, `title`, `status`, `summary`,
`created_at`, `updated_at`, `last_message_at`, and redaction-safe `metadata`.

Indexes: `(tenant_id, actor_user_id, updated_at desc)` and status/time for
retention. A unique policy may be added later for a user-visible slug; do not
use an email as an owner key.

### `ai.messages`

`id uuid`, `conversation_id uuid`, `sequence bigint`, `run_id uuid`, `role`,
`content_type`, redacted `content`, `content_json`, `model_id`, `prompt_version`,
`created_at`.

Unique `(conversation_id, sequence)` prevents ambiguous ordering. Tool calls
and results should be referenced by `run_id`/`tool_execution_id`, not embedded
as unrestricted JSON only.

### `ai.runs`

`id uuid`, `conversation_id uuid`, `tenant_id uuid`, `actor_user_id uuid`,
`status`, `agent_id`, `protocol_version`, `provider`, `model_id`, `started_at`,
`finished_at`, `last_event_sequence`, usage metadata, error code, and
`idempotency_key`.

Unique `(actor_user_id, idempotency_key)` where present. Do not store raw
provider request/response bodies by default.

### `ai.tool_executions`

`id uuid`, `run_id uuid`, `tool_name`, `tool_version`, `risk`, `status`,
`arguments_redacted`, `result_redacted`, `policy_decision`, `approval_id`,
`idempotency_key`, `started_at`, `finished_at`, `error_code`.

Index by tenant/time, run, and tool/status. Keep the original argument hash for
replay detection without retaining sensitive arguments indefinitely.

### `ai.approvals`

`id uuid`, `run_id uuid`, `tool_execution_id uuid`, `tenant_id uuid`,
`requester_user_id uuid`, `approver_user_id uuid`, `status`, `summary_redacted`,
`resource_version`, `permission_version`, `expires_at`, `consumed_at`, and
`created_at`.

Approval consumption must be a transactionally guarded state transition.

### `ai.knowledge_sources`

`id uuid`, `tenant_id uuid nullable`, `scope`, `source_type`, `source_key`,
`title`, `owner`, `classification`, `status`, `version`, `checksum`,
`effective_from`, `effective_to`, and timestamps.

### `ai.knowledge_chunks`

`id uuid`, `source_id uuid`, `tenant_id uuid nullable`, `chunk_index`, `heading`,
`content`, `content_checksum`, token count, ACL metadata, embedding set/model
metadata, and timestamps.

The vector column and index are intentionally undecided until the model and
dimension are selected. A text-search column may be added for hybrid retrieval.

### `ai.feedback`

`id uuid`, `tenant_id uuid`, `actor_user_id uuid`, `conversation_id uuid`,
`message_id uuid`, rating, reason, redacted comment, and `created_at`.

## PostgreSQL/pgvector gates

Before any `CREATE EXTENSION vector` migration:

1. Verify the deployed CloudNativePG PostgreSQL 18 image supports the approved
   extension version.
2. Confirm extension policy, backup/restore behavior, and operator support.
3. Select one embedding model/dimension and record it in the design ADR.
4. Benchmark index/build time, query latency, storage, and tenant filtering.
5. Test the migration on an isolated restore and a disposable database.
6. Add monitoring and a forward-compatible rollback plan.

Do not add a guessed vector dimension or enable the extension on the shared
cluster during the documentation phase.

## Migration rules

- Additive expand/migrate/contract only.
- Use PostgreSQL 18-compatible `uuidv7()` consistently with current Arda
  migrations if the service standard accepts it.
- No `DROP ... CASCADE` or data cleanup in the first rollout.
- Backfill in bounded batches with counts/checksums and an operator record.
- Code must tolerate nullable/new columns during rollout.
- A failed migration triggers forward recovery or restore procedure; never a
  blind destructive rollback on production data.
