# AI database design

Status: approved for the Gate 2 persistence foundation and the additive
pgvector capability enablement. The extension is enabled separately from the
embedding column/index; later provider/RAG changes require their own review
gates in [rollout-plan.md](rollout-plan.md).

## Ownership decision

Create a service-owned PostgreSQL database named `ai`. Keep application tables
in PostgreSQL `public` with an `ai_` prefix, matching the existing Arda service
convention. The AI service owns migrations and the database role; it does not
query another service's tables.

The database boundary is the isolation boundary; the `ai_` prefix prevents
name collisions in `public`. Store stable foreign IDs and resolve display data
through contracts. `public.goose_db_version` is migration bookkeeping, not an
AI application table.

## Proposed tables

### `public.ai_conversations`

`id uuid`, `tenant_id varchar(64)`, `actor_user_id uuid`, `title`, `status`, `summary`,
`created_at`, `updated_at`, `last_message_at`, and redaction-safe `metadata`.

Indexes: `(tenant_id, actor_user_id, updated_at desc)` and status/time for
retention. A unique policy may be added later for a user-visible slug; do not
use an email as an owner key.

### `public.ai_messages`

`id uuid`, `conversation_id uuid`, `sequence bigint`, `run_id uuid`, `role`,
`content_type`, redacted `content`, `content_json`, `model_id`, `prompt_version`,
`created_at`.

Unique `(conversation_id, sequence)` prevents ambiguous ordering. Tool calls
and results should be referenced by `run_id`/`tool_execution_id`, not embedded
as unrestricted JSON only.

### `public.ai_runs`

`id uuid`, `conversation_id uuid`, `tenant_id varchar(64)`, `actor_user_id uuid`,
`status`, `agent_id`, `protocol_version`, `provider`, `model_id`, `started_at`,
`finished_at`, `last_event_sequence`, usage metadata, error code, and
`idempotency_key`.

Unique `(actor_user_id, idempotency_key)` where present. Do not store raw
provider request/response bodies by default.

### `public.ai_tool_executions`

`id uuid`, `run_id uuid`, `tenant_id varchar(64)`, `tool_name`, `tool_version`, `risk`, `status`,
`arguments_redacted`, `result_redacted`, `policy_decision`, `approval_id`,
`idempotency_key`, `started_at`, `finished_at`, `error_code`.

Index by tenant/time, run, and tool/status. Keep the original argument hash for
replay detection without retaining sensitive arguments indefinitely.

### `public.ai_approvals`

`id uuid`, `run_id uuid`, `tool_execution_id uuid`, `tenant_id varchar(64)`,
`requester_user_id uuid`, `approver_user_id uuid`, `status`, `summary_redacted`,
`resource_version`, `permission_version`, `expires_at`, `consumed_at`, and
`created_at`.

Approval consumption must be a transactionally guarded state transition.

### `public.ai_knowledge_sources`

`id uuid`, `tenant_id varchar(64) nullable`, `scope`, `source_type`, `source_key`,
`title`, `owner`, `classification`, `status`, `version`, `checksum`,
`effective_from`, `effective_to`, and timestamps.

### `public.ai_knowledge_chunks`

`id uuid`, `source_id uuid`, `tenant_id varchar(64) nullable`, `chunk_index`, `heading`,
`content`, `content_checksum`, token count, ACL metadata, embedding set/model
metadata, and timestamps.

The vector column and index are intentionally undecided until the model and
dimension are selected. A text-search column may be added for hybrid retrieval.

### `public.ai_feedback`

`id uuid`, `tenant_id varchar(64)`, `actor_user_id uuid`, `conversation_id uuid`,
`message_id uuid`, rating, reason, redacted comment, and `created_at`.

## PostgreSQL/pgvector gates

The extension enablement migration is complete after these checks:

1. Verify the deployed CloudNativePG PostgreSQL 18 image supports the approved
   extension version.
2. Confirm extension policy, backup/restore behavior, and operator support.
3. Apply `CREATE EXTENSION IF NOT EXISTS vector` through the AI service Goose
   migration and verify the installed version in the live database.

The following gates remain open before adding a vector column or index:

1. Select one embedding model/dimension and record it in the design ADR.
2. Benchmark index/build time, query latency, storage, and tenant filtering.
3. Test the vector migration on an isolated restore and a disposable database.
4. Add monitoring and a forward-compatible rollback plan.

Do not add a guessed vector dimension or provider credential. The extension
alone has no effect on current full-text retrieval or production requests.

## Migration rules

- Additive expand/migrate/contract only.
- Use PostgreSQL 18-compatible `uuidv7()` consistently with current Arda
  migrations if the service standard accepts it.
- No `DROP ... CASCADE` or data cleanup in the first rollout.
- Backfill in bounded batches with counts/checksums and an operator record.
- Code must tolerate nullable/new columns during rollout.
- A failed migration triggers forward recovery or restore procedure; never a
  blind destructive rollback on production data.
