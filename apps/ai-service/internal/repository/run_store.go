package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRunAlreadyExists     = errors.New("AI run already exists")
	ErrConversationNotFound = errors.New("AI conversation not found")
)

// RunContext contains only server-resolved ownership data and protocol IDs.
type RunContext struct {
	TenantID       string
	ActorUserID    string
	ExternalThread string
	ExternalRun    string
}

// RunStore persists the minimum transcript/run state needed to resume safely.
type RunStore interface {
	Start(ctx context.Context, run RunContext, userMessage string) error
	Finish(ctx context.Context, run RunContext, assistantMessage, status string) error
}

type ToolExecutionStore interface {
	StartTool(ctx context.Context, run RunContext, toolName string, toolVersion int, risk, policyDecision, argumentsRedacted string) (string, error)
	FinishTool(ctx context.Context, executionID, status, resultRedacted, errorCode string) error
}

type HistoryMessage struct {
	Role    string
	Content string
}

type HistoryStore interface {
	RecentMessages(ctx context.Context, run RunContext, limit int) ([]HistoryMessage, error)
}

// ToolActivityStore replays a compact tool-activity log from earlier turns.
// Providers reject unpaired tool messages, so the log is injected as ordinary
// system context instead of reconstructed tool_calls/tool pairs.
type ToolActivityStore interface {
	RecentToolSummaries(ctx context.Context, run RunContext, limit int) ([]HistoryMessage, error)
}

type UsageSetter interface {
	SetUsage(ctx context.Context, run RunContext, usageJSON string) error
}

type ModelSetter interface {
	SetModel(ctx context.Context, run RunContext, provider, modelID string) error
}

type RunFailureSetter interface {
	FailRun(ctx context.Context, run RunContext, errorCode string) error
}

type ConversationSummary struct {
	ThreadID      string `json:"threadId"`
	Title         string `json:"title"`
	MessageCount  int    `json:"messageCount"`
	LastMessageAt string `json:"lastMessageAt,omitempty"`
	DeletedAt     string `json:"deletedAt,omitempty"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
	Status        string `json:"status"`
}

type ConversationMessage struct {
	Sequence  int64  `json:"sequence"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
	// Artifacts are the renderable tool outputs of the run (report
	// presentations / charts). They are replayed in the UI when a thread is
	// reopened, so a chart or table is not lost on reload.
	Artifacts []json.RawMessage `json:"artifacts,omitempty"`
}

type ConversationReader interface {
	ListConversations(ctx context.Context, tenantID, actorUserID string, limit int) ([]ConversationSummary, error)
	ConversationMessages(ctx context.Context, tenantID, actorUserID, threadID string, limit int) ([]ConversationMessage, error)
}

type ConversationMutator interface {
	DeleteConversation(ctx context.Context, tenantID, actorUserID, threadID string) error
}

// ConversationTrash provides scoped recovery and permanent-delete operations
// for conversations that have already been soft-deleted.
type ConversationTrash interface {
	ListDeletedConversations(ctx context.Context, tenantID, actorUserID string, limit int) ([]ConversationSummary, error)
	RestoreConversation(ctx context.Context, tenantID, actorUserID, threadID string) error
	PermanentlyDeleteConversation(ctx context.Context, tenantID, actorUserID, threadID string) error
	PermanentlyDeleteAllDeletedConversations(ctx context.Context, tenantID, actorUserID string) (int64, error)
}

type ConversationRetentionStore interface {
	GetConversationRetention(ctx context.Context, tenantID string) (int, error)
	SaveConversationRetention(ctx context.Context, tenantID string, months int) error
}

type SQLRunStore struct {
	db               *sql.DB
	encryptionSecret string
}

func NewSQLRunStore(db *sql.DB) *SQLRunStore {
	return &SQLRunStore{db: db}
}

func (s *SQLRunStore) SetEncryptionSecret(secret string) {
	if s != nil {
		s.encryptionSecret = secret
	}
}

func (s *SQLRunStore) Start(ctx context.Context, run RunContext, userMessage string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return fmt.Errorf("user message is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin AI run transaction: %w", err)
	}
	defer tx.Rollback()

	var conversationID string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text
		FROM public.ai_conversations
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_thread_id = $3
		FOR UPDATE
	`, run.TenantID, run.ActorUserID, run.ExternalThread).Scan(&conversationID)
	if err == sql.ErrNoRows {
		title := userMessage
		// Cut by runes, not bytes: a byte slice through Vietnamese text can
		// split a multi-byte character and the INSERT then fails with
		// "invalid byte sequence for encoding UTF8" (0xc3...), 503-ing the
		// whole run at Start.
		runes := []rune(title)
		if len(runes) > 80 {
			title = string(runes[:80])
		}
		err = tx.QueryRowContext(ctx, `
			INSERT INTO public.ai_conversations (tenant_id, actor_user_id, external_thread_id, title, last_message_at)
			VALUES ($1, $2, $3, NULLIF($4, ''), now())
			RETURNING id::text
		`, run.TenantID, run.ActorUserID, run.ExternalThread, title).Scan(&conversationID)
	}
	if err != nil {
		return fmt.Errorf("resolve AI conversation: %w", err)
	}

	var internalRunID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.ai_runs
			(conversation_id, tenant_id, actor_user_id, external_run_id, status)
		VALUES ($1, $2, $3, $4, 'RUNNING')
		ON CONFLICT (tenant_id, external_run_id) DO NOTHING
		RETURNING id::text
	`, conversationID, run.TenantID, run.ActorUserID, run.ExternalRun).Scan(&internalRunID)
	if err == sql.ErrNoRows {
		return ErrRunAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("persist AI run: %w", err)
	}

	if err := insertMessage(ctx, tx, conversationID, internalRunID, "user", userMessage); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_conversations SET updated_at = now(), last_message_at = now() WHERE id = $1
	`, conversationID); err != nil {
		return fmt.Errorf("update AI conversation timestamp: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit AI run start: %w", err)
	}
	return nil
}

func (s *SQLRunStore) Finish(ctx context.Context, run RunContext, assistantMessage, status string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	assistantMessage = strings.TrimSpace(assistantMessage)
	if assistantMessage == "" {
		return fmt.Errorf("assistant message is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin AI run finish transaction: %w", err)
	}
	defer tx.Rollback()

	var internalRunID, conversationID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id::text, conversation_id::text
		FROM public.ai_runs
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
		FOR UPDATE
	`, run.TenantID, run.ActorUserID, run.ExternalRun).Scan(&internalRunID, &conversationID); err != nil {
		return fmt.Errorf("resolve AI run for finish: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_runs
		SET status = $1, finished_at = now(), last_event_sequence = last_event_sequence + 1
		WHERE id = $2
	`, status, internalRunID); err != nil {
		return fmt.Errorf("finish AI run: %w", err)
	}
	if err := insertMessage(ctx, tx, conversationID, internalRunID, "assistant", assistantMessage); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_conversations SET updated_at = now(), last_message_at = now() WHERE id = $1
	`, conversationID); err != nil {
		return fmt.Errorf("update AI conversation after finish: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit AI run finish: %w", err)
	}
	return nil
}

func (s *SQLRunStore) StartTool(ctx context.Context, run RunContext, toolName string, toolVersion int, risk, policyDecision, argumentsRedacted string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("AI run store is not configured")
	}
	var executionID string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_tool_executions
			(run_id, tenant_id, actor_user_id, tool_name, tool_version, risk, status, arguments_redacted, policy_decision, started_at)
		SELECT id, tenant_id, actor_user_id, $4, $5, $6, 'REQUESTED', $7::jsonb, $8, now()
		FROM public.ai_runs
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
		RETURNING id::text
	`, run.TenantID, run.ActorUserID, run.ExternalRun, toolName, fmt.Sprint(toolVersion), risk, jsonObject(argumentsRedacted), policyDecision).Scan(&executionID)
	if err != nil {
		return "", fmt.Errorf("persist AI tool execution: %w", err)
	}
	return executionID, nil
}

func (s *SQLRunStore) FinishTool(ctx context.Context, executionID, status, resultRedacted, errorCode string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_tool_executions
		SET status = $1, result_redacted = $2::jsonb, error_code = NULLIF($3, ''), finished_at = now()
		WHERE id = $4
	`, status, jsonObject(resultRedacted), errorCode, executionID); err != nil {
		return fmt.Errorf("finish AI tool execution: %w", err)
	}
	return nil
}

func jsonObject(value string) string {
	if strings.TrimSpace(value) == "" {
		return `{}`
	}
	return value
}

func (s *SQLRunStore) RecentMessages(ctx context.Context, run RunContext, limit int) ([]HistoryMessage, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("AI run store is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.role, m.content
		FROM public.ai_messages m
		JOIN public.ai_runs r ON r.id = m.run_id
		JOIN public.ai_conversations c ON c.id = r.conversation_id
		WHERE c.tenant_id = $1 AND c.actor_user_id = $2 AND c.external_thread_id = $3
		  AND r.status IN ('SUCCEEDED', 'FAILED')
		ORDER BY r.started_at DESC, m.sequence DESC
		LIMIT $4
	`, run.TenantID, run.ActorUserID, run.ExternalThread, limit)
	if err != nil {
		return nil, fmt.Errorf("load AI history: %w", err)
	}
	defer rows.Close()

	var messages []HistoryMessage
	for rows.Next() {
		var item HistoryMessage
		if err := rows.Scan(&item.Role, &item.Content); err != nil {
			return nil, fmt.Errorf("scan AI history: %w", err)
		}
		messages = append(messages, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI history: %w", err)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// RecentToolSummaries returns the most recent tool executions in the
// conversation (newest last) with a bounded preview of the redacted result.
func (s *SQLRunStore) RecentToolSummaries(ctx context.Context, run RunContext, limit int) ([]HistoryMessage, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("AI run store is not configured")
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.tool_name, t.status, left(coalesce(t.result_redacted::text, ''), 400)
		FROM public.ai_tool_executions t
		JOIN public.ai_runs r ON r.id = t.run_id
		JOIN public.ai_conversations c ON c.id = r.conversation_id
		WHERE c.tenant_id = $1 AND c.actor_user_id = $2 AND c.external_thread_id = $3
		  AND r.status IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'WAITING_APPROVAL')
		ORDER BY t.started_at DESC
		LIMIT $4
	`, run.TenantID, run.ActorUserID, run.ExternalThread, limit)
	if err != nil {
		return nil, fmt.Errorf("load AI tool summaries: %w", err)
	}
	defer rows.Close()

	var messages []HistoryMessage
	for rows.Next() {
		var name, status, preview string
		if err := rows.Scan(&name, &status, &preview); err != nil {
			return nil, fmt.Errorf("scan AI tool summary: %w", err)
		}
		preview = strings.TrimSpace(preview)
		if preview == "" || preview == "{}" {
			preview = "(no output)"
		}
		messages = append(messages, HistoryMessage{
			Role:    "system",
			Content: fmt.Sprintf("[tool %s · %s] %s", name, status, preview),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI tool summaries: %w", err)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// RunMessages loads the persisted messages of one run in chronological order,
// regardless of the run status (RecentMessages only serves finished runs).
func (s *SQLRunStore) RunMessages(ctx context.Context, run RunContext) ([]HistoryMessage, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("AI run store is not configured")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.role, m.content
		FROM public.ai_messages m
		JOIN public.ai_runs r ON r.id = m.run_id
		WHERE r.tenant_id = $1 AND r.actor_user_id = $2 AND r.external_run_id = $3
		ORDER BY m.sequence ASC
		LIMIT 100
	`, run.TenantID, run.ActorUserID, run.ExternalRun)
	if err != nil {
		return nil, fmt.Errorf("load AI run messages: %w", err)
	}
	defer rows.Close()

	var messages []HistoryMessage
	for rows.Next() {
		var item HistoryMessage
		if err := rows.Scan(&item.Role, &item.Content); err != nil {
			return nil, fmt.Errorf("scan AI run message: %w", err)
		}
		messages = append(messages, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI run messages: %w", err)
	}
	return messages, nil
}

// ResumeRun returns a WAITING_APPROVAL run (and its pending tool execution)
// to RUNNING so the agent loop can continue after an approval is executed.
func (s *SQLRunStore) ResumeRun(ctx context.Context, run RunContext) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_runs SET status = 'RUNNING', finished_at = NULL
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
		  AND status = 'WAITING_APPROVAL'
	`, run.TenantID, run.ActorUserID, run.ExternalRun)
	if err != nil {
		return fmt.Errorf("resume AI run: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("AI run not awaiting approval")
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE public.ai_tool_executions te
		SET status = 'RUNNING'
		FROM public.ai_runs r
		WHERE te.run_id = r.id
		  AND r.tenant_id = $1 AND r.actor_user_id = $2 AND r.external_run_id = $3
		  AND te.status = 'WAITING_APPROVAL'
	`, run.TenantID, run.ActorUserID, run.ExternalRun)
	if err != nil {
		return fmt.Errorf("resume AI tool execution: %w", err)
	}
	return nil
}

func (s *SQLRunStore) SetUsage(ctx context.Context, run RunContext, usageJSON string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_runs SET usage = $4::jsonb
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
	`, run.TenantID, run.ActorUserID, run.ExternalRun, jsonObject(usageJSON))
	if err != nil {
		return fmt.Errorf("persist AI usage: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("AI run not found for usage update")
	}
	return nil
}

func (s *SQLRunStore) SetModel(ctx context.Context, run RunContext, provider, modelID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_runs SET provider = NULLIF($4, ''), model_id = NULLIF($5, '')
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
	`, run.TenantID, run.ActorUserID, run.ExternalRun, strings.TrimSpace(provider), strings.TrimSpace(modelID))
	if err != nil {
		return fmt.Errorf("persist AI model metadata: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("AI run not found for model metadata")
	}
	return nil
}

func (s *SQLRunStore) FailRun(ctx context.Context, run RunContext, errorCode string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_runs
		SET status = 'FAILED', error_code = NULLIF($4, ''), finished_at = now()
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
		  AND status IN ('WAITING_APPROVAL', 'RUNNING')
	`, run.TenantID, run.ActorUserID, run.ExternalRun, strings.TrimSpace(errorCode))
	if err != nil {
		return fmt.Errorf("fail AI run: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("AI run not found or already terminal")
	}
	return nil
}

func (s *SQLRunStore) ListConversations(ctx context.Context, tenantID, actorUserID string, limit int) ([]ConversationSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("AI run store is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.external_thread_id,
		       COALESCE(c.title, LEFT(COALESCE((
		           SELECT content FROM public.ai_messages m
		           WHERE m.conversation_id = c.id ORDER BY m.sequence ASC LIMIT 1
		       ), ''), 120)), 
		       (SELECT COUNT(*) FROM public.ai_messages m WHERE m.conversation_id = c.id),
		       COALESCE(c.last_message_at::text, ''),
		       c.status
		FROM public.ai_conversations c
		WHERE c.tenant_id = $1 AND c.actor_user_id = $2 AND c.status = 'ACTIVE'
		ORDER BY COALESCE(c.last_message_at, c.updated_at) DESC
		LIMIT $3
	`, tenantID, actorUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("list AI conversations: %w", err)
	}
	defer rows.Close()

	items := make([]ConversationSummary, 0, limit)
	for rows.Next() {
		var item ConversationSummary
		if err := rows.Scan(&item.ThreadID, &item.Title, &item.MessageCount, &item.LastMessageAt, &item.Status); err != nil {
			return nil, fmt.Errorf("scan AI conversation: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLRunStore) ConversationMessages(ctx context.Context, tenantID, actorUserID, threadID string, limit int) ([]ConversationMessage, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("AI run store is not configured")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.sequence, m.role, m.content, m.created_at::text,
		       COALESCE(arts.artifacts, '[]'::jsonb)
		FROM public.ai_messages m
		JOIN public.ai_conversations c ON c.id = m.conversation_id
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(jsonb_build_object('tool_name', a.tool_name, 'result', a.payload) ORDER BY a.finished_at NULLS LAST) AS artifacts
			FROM (
				SELECT t.tool_name, t.finished_at,
				       CASE WHEN t.tool_name = 'execute' THEN t.result_redacted->'output' ELSE t.result_redacted END AS payload
				FROM public.ai_tool_executions t
				WHERE t.run_id = m.run_id
				  AND m.role = 'assistant'
				  AND t.status = 'SUCCEEDED'
				  AND t.tool_name IN ('execute', 'renderChart')
				  AND t.result_redacted IS NOT NULL
			) a
			WHERE (a.payload ? 'chart') OR ((a.payload ? 'columns') AND (a.payload ? 'rows'))
		) arts ON TRUE
		WHERE c.tenant_id = $1 AND c.actor_user_id = $2 AND c.external_thread_id = $3
		ORDER BY m.sequence ASC
		LIMIT $4
	`, tenantID, actorUserID, threadID, limit)
	if err != nil {
		return nil, fmt.Errorf("list AI conversation messages: %w", err)
	}
	defer rows.Close()

	items := make([]ConversationMessage, 0, limit)
	for rows.Next() {
		var item ConversationMessage
		var artifactsRaw []byte
		if err := rows.Scan(&item.Sequence, &item.Role, &item.Content, &item.CreatedAt, &artifactsRaw); err != nil {
			return nil, fmt.Errorf("scan AI message: %w", err)
		}
		item.Artifacts = selectArtifacts(artifactsRaw)
		items = append(items, item)
	}
	return items, rows.Err()
}

// storedArtifact is one renderable tool output persisted for a run.
type storedArtifact struct {
	ToolName string          `json:"tool_name"`
	Result   json.RawMessage `json:"result"`
}

// selectArtifacts picks the artifacts worth replaying in a reopened thread.
// A run that asked for a report produces both an `execute` presentation and a
// `renderChart` call; preferring renderChart avoids rendering the same chart
// twice. The list is capped so a long agent run cannot flood the transcript.
func selectArtifacts(raw []byte) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var items []storedArtifact
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	var chosen []json.RawMessage
	for _, item := range items {
		if item.ToolName == "renderChart" && len(item.Result) > 0 {
			chosen = append(chosen, item.Result)
		}
	}
	if len(chosen) == 0 {
		// Fall back to the last execute presentation that carries a chart.
		for index := len(items) - 1; index >= 0; index-- {
			if items[index].ToolName == "execute" && len(items[index].Result) > 0 && hasJSONKey(items[index].Result, "chart") {
				chosen = append(chosen, items[index].Result)
				break
			}
		}
	}
	if len(chosen) == 0 {
		for index := len(items) - 1; index >= 0; index-- {
			if len(items[index].Result) > 0 {
				chosen = append(chosen, items[index].Result)
				break
			}
		}
	}
	if len(chosen) > 2 {
		chosen = chosen[:2]
	}
	return chosen
}

func hasJSONKey(raw json.RawMessage, key string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	_, ok := object[key]
	return ok
}

func insertMessage(ctx context.Context, tx *sql.Tx, conversationID, runID, role, content string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.ai_messages (conversation_id, run_id, sequence, role, content)
		SELECT $1, $2, COALESCE(MAX(sequence), 0) + 1, $3, $4
		FROM public.ai_messages
		WHERE conversation_id = $1
	`, conversationID, runID, role, content); err != nil {
		return fmt.Errorf("persist AI %s message: %w", role, err)
	}
	return nil
}

func (s *SQLRunStore) DeleteConversation(ctx context.Context, tenantID, actorUserID, threadID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_conversations SET status = 'DELETED', deleted_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_thread_id = $3 AND status = 'ACTIVE'
	`, tenantID, actorUserID, threadID)
	if err != nil {
		return fmt.Errorf("delete AI conversation: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrConversationNotFound
	}
	return nil
}

// ListDeletedConversations lists the tenant's trashed conversations (most
// recently deleted first) so the UI can offer restore.
func (s *SQLRunStore) ListDeletedConversations(ctx context.Context, tenantID, actorUserID string, limit int) ([]ConversationSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("AI run store is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.external_thread_id,
		       COALESCE(c.title, LEFT(COALESCE((
		           SELECT content FROM public.ai_messages m
		           WHERE m.conversation_id = c.id ORDER BY m.sequence ASC LIMIT 1
		       ), ''), 120)),
		       (SELECT COUNT(*) FROM public.ai_messages m WHERE m.conversation_id = c.id),
		       COALESCE(c.last_message_at::text, ''),
		       COALESCE(c.deleted_at::text, ''),
		       COALESCE((c.deleted_at + make_interval(months => COALESCE(s.trash_retention_months, 1)))::text, ''),
		       c.status
		FROM public.ai_conversations c
		LEFT JOIN public.ai_conversation_settings s ON s.tenant_id = c.tenant_id
		WHERE c.tenant_id = $1 AND c.actor_user_id = $2 AND c.status = 'DELETED'
		ORDER BY c.updated_at DESC
		LIMIT $3
	`, tenantID, actorUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("list deleted AI conversations: %w", err)
	}
	defer rows.Close()

	items := make([]ConversationSummary, 0, limit)
	for rows.Next() {
		var item ConversationSummary
		if err := rows.Scan(&item.ThreadID, &item.Title, &item.MessageCount, &item.LastMessageAt, &item.DeletedAt, &item.ExpiresAt, &item.Status); err != nil {
			return nil, fmt.Errorf("scan deleted AI conversation: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RestoreConversation brings a trashed conversation back to ACTIVE.
func (s *SQLRunStore) RestoreConversation(ctx context.Context, tenantID, actorUserID, threadID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_conversations SET status = 'ACTIVE', deleted_at = NULL, updated_at = now()
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_thread_id = $3 AND status = 'DELETED'
	`, tenantID, actorUserID, threadID)
	if err != nil {
		return fmt.Errorf("restore AI conversation: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrConversationNotFound
	}
	return nil
}

type AnalyticsSummary struct {
	TotalRuns        int64              `json:"totalRuns"`
	SuccessfulRuns   int64              `json:"successfulRuns"`
	FailedRuns       int64              `json:"failedRuns"`
	SuccessRate      float64            `json:"successRate"`
	TotalTokens      int64              `json:"totalTokens"`
	PromptTokens     int64              `json:"promptTokens"`
	CompletionTokens int64              `json:"completionTokens"`
	Latency          LatencyStats       `json:"latency"`
	Feedback         FeedbackStats      `json:"feedback"`
	RAGQuality       RAGQualityStats    `json:"ragQuality"`
	RunsByDay        []DayTrend         `json:"runsByDay"`
	ModelsByUsage    []ModelUsage       `json:"modelsByUsage"`
	Decision         DecisionUsageStats `json:"decision"`
	Agentic          AgenticUsageStats  `json:"agentic"`
}

type DecisionUsageStats struct {
	Evaluations       int64   `json:"evaluations"`
	Routed            int64   `json:"routed"`
	LowConfidence     int64   `json:"low_confidence"`
	Tokens            int64   `json:"tokens"`
	AvgLatencyMs      int64   `json:"avg_latency_ms"`
	AverageConfidence float64 `json:"average_confidence"`
}

type AgenticUsageStats struct {
	ToolCalls        int64 `json:"tool_calls"`
	SuccessfulCalls  int64 `json:"successful_calls"`
	FailedCalls      int64 `json:"failed_calls"`
	PendingApprovals int64 `json:"pending_approvals"`
	ApprovedActions  int64 `json:"approved_actions"`
	RejectedActions  int64 `json:"rejected_actions"`
}

type LatencyStats struct {
	P50Ms int64 `json:"p50Ms"`
	P95Ms int64 `json:"p95Ms"`
	P99Ms int64 `json:"p99Ms"`
	AvgMs int64 `json:"avgMs"`
}

type FeedbackStats struct {
	Total            int64   `json:"total"`
	Positive         int64   `json:"positive"`
	Negative         int64   `json:"negative"`
	SatisfactionRate float64 `json:"satisfactionRate"`
}

type RAGQualityStats struct {
	GroundednessScore  float64 `json:"groundednessScore"`
	FaithfulnessScore  float64 `json:"faithfulnessScore"`
	RetrievalPrecision float64 `json:"retrievalPrecision"`
}

type DayTrend struct {
	Date   string `json:"date"`
	Runs   int64  `json:"runs"`
	Tokens int64  `json:"tokens"`
	Errors int64  `json:"errors"`
}

type ModelUsage struct {
	ModelID  string `json:"modelId"`
	Provider string `json:"provider"`
	Runs     int64  `json:"runs"`
	Tokens   int64  `json:"tokens"`
}

func (s *SQLRunStore) GetAnalytics(ctx context.Context, tenantID string) (*AnalyticsSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("analytics persistence is unavailable")
	}
	summary := &AnalyticsSummary{RunsByDay: []DayTrend{}, ModelsByUsage: []ModelUsage{}}

	var totalRuns, successRuns, failedRuns int64
	var totalTokens, promptTokens, completionTokens int64
	var avgLatency float64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status = 'SUCCEEDED'),
			count(*) FILTER (WHERE status = 'FAILED'),
			coalesce(avg(EXTRACT(EPOCH FROM (finished_at - started_at)) * 1000) FILTER (WHERE finished_at IS NOT NULL), 0),
			coalesce(sum(CASE WHEN usage->>'total_tokens' ~ '^[0-9]+$' THEN (usage->>'total_tokens')::bigint ELSE 0 END), 0),
			coalesce(sum(CASE WHEN usage->>'prompt_tokens' ~ '^[0-9]+$' THEN (usage->>'prompt_tokens')::bigint ELSE 0 END), 0),
			coalesce(sum(CASE WHEN usage->>'completion_tokens' ~ '^[0-9]+$' THEN (usage->>'completion_tokens')::bigint ELSE 0 END), 0)
		FROM public.ai_runs
		WHERE tenant_id = $1 OR $1 = ''
	`, tenantID).Scan(&totalRuns, &successRuns, &failedRuns, &avgLatency, &totalTokens, &promptTokens, &completionTokens)

	if err != nil {
		return nil, fmt.Errorf("load analytics runs: %w", err)
	}
	if totalRuns > 0 {
		summary.TotalRuns = totalRuns
		summary.SuccessfulRuns = successRuns
		summary.FailedRuns = failedRuns
		summary.SuccessRate = float64(successRuns) / float64(totalRuns) * 100.0
		summary.Latency.AvgMs = int64(avgLatency)
	}
	summary.TotalTokens = totalTokens
	summary.PromptTokens = promptTokens
	summary.CompletionTokens = completionTokens

	modelRows, modelErr := s.db.QueryContext(ctx, `
		SELECT COALESCE(model_id, ''), COALESCE(provider, ''), count(*),
		       COALESCE(sum(CASE WHEN usage->>'total_tokens' ~ '^[0-9]+$' THEN (usage->>'total_tokens')::bigint ELSE 0 END), 0)
		FROM public.ai_runs
		WHERE tenant_id = $1 OR $1 = ''
		GROUP BY model_id, provider
		ORDER BY count(*) DESC, model_id ASC
	`, tenantID)
	if modelErr != nil {
		return nil, fmt.Errorf("load analytics models: %w", modelErr)
	}
	for modelRows.Next() {
		var item ModelUsage
		if err := modelRows.Scan(&item.ModelID, &item.Provider, &item.Runs, &item.Tokens); err != nil {
			modelRows.Close()
			return nil, fmt.Errorf("scan analytics model: %w", err)
		}
		summary.ModelsByUsage = append(summary.ModelsByUsage, item)
	}
	if err := modelRows.Err(); err != nil {
		modelRows.Close()
		return nil, fmt.Errorf("iterate analytics models: %w", err)
	}
	modelRows.Close()

	dayRows, dayErr := s.db.QueryContext(ctx, `
		SELECT to_char(started_at AT TIME ZONE 'Asia/Ho_Chi_Minh' /* ardatime.DefaultTimezoneName */, 'YYYY-MM-DD'), count(*),
		       COALESCE(sum(CASE WHEN usage->>'total_tokens' ~ '^[0-9]+$' THEN (usage->>'total_tokens')::bigint ELSE 0 END), 0),
		       count(*) FILTER (WHERE status = 'FAILED')
		FROM public.ai_runs
		WHERE (tenant_id = $1 OR $1 = '') AND started_at >= now() - interval '30 days'
		GROUP BY 1 ORDER BY 1 ASC
	`, tenantID)
	if dayErr != nil {
		return nil, fmt.Errorf("load analytics daily trend: %w", dayErr)
	}
	for dayRows.Next() {
		var item DayTrend
		if err := dayRows.Scan(&item.Date, &item.Runs, &item.Tokens, &item.Errors); err != nil {
			dayRows.Close()
			return nil, fmt.Errorf("scan analytics daily trend: %w", err)
		}
		summary.RunsByDay = append(summary.RunsByDay, item)
	}
	if err := dayRows.Err(); err != nil {
		dayRows.Close()
		return nil, fmt.Errorf("iterate analytics daily trend: %w", err)
	}
	dayRows.Close()

	var fbTotal, fbPos, fbNeg int64
	fbErr := s.db.QueryRowContext(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE helpful = true),
			count(*) FILTER (WHERE helpful = false)
		FROM (
			SELECT f.helpful AS helpful
			FROM public.ai_rag_feedback f
			JOIN public.ai_rag_runs r ON r.id = f.run_id
			WHERE r.tenant_id = $1
			UNION ALL
			SELECT (fb.rating >= 4) AS helpful
			FROM public.ai_feedback fb
			WHERE fb.tenant_id = $1
		) ratings
	`, tenantID).Scan(&fbTotal, &fbPos, &fbNeg)

	if fbErr == nil && fbTotal > 0 {
		summary.Feedback.Total = fbTotal
		summary.Feedback.Positive = fbPos
		summary.Feedback.Negative = fbNeg
		summary.Feedback.SatisfactionRate = float64(fbPos) / float64(fbTotal) * 100.0
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE decision_usage <> '{}'::jsonb),
		       count(*) FILTER (WHERE decision_usage->>'skill' IS NOT NULL AND decision_usage->>'skill' <> 'general'),
		       count(*) FILTER (WHERE decision_usage->>'low_confidence' = 'true'),
	       COALESCE(sum(CASE WHEN decision_usage->>'input_tokens' ~ '^[0-9]+$' THEN (decision_usage->>'input_tokens')::bigint ELSE 0 END), 0)
	       + COALESCE(sum(CASE WHEN decision_usage->>'output_tokens' ~ '^[0-9]+$' THEN (decision_usage->>'output_tokens')::bigint ELSE 0 END), 0),
	       COALESCE(avg(CASE WHEN decision_usage->>'latency_ms' ~ '^[0-9]+$' THEN (decision_usage->>'latency_ms')::double precision END), 0)::bigint,
	       COALESCE(avg(CASE WHEN decision_usage->>'confidence' ~ '^(0(\.[0-9]+)?|1(\.0+)?)$' THEN (decision_usage->>'confidence')::double precision END), 0)
		FROM public.ai_runs WHERE tenant_id = $1
	`, tenantID).Scan(&summary.Decision.Evaluations, &summary.Decision.Routed, &summary.Decision.LowConfidence,
		&summary.Decision.Tokens, &summary.Decision.AvgLatencyMs, &summary.Decision.AverageConfidence)
	if err != nil {
		return nil, fmt.Errorf("load decision analytics: %w", err)
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (WHERE status = 'SUCCEEDED'), count(*) FILTER (WHERE status = 'FAILED')
		FROM public.ai_tool_executions WHERE tenant_id = $1
	`).Scan(&summary.Agentic.ToolCalls, &summary.Agentic.SuccessfulCalls, &summary.Agentic.FailedCalls)
	if err != nil {
		return nil, fmt.Errorf("load AI tool analytics: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE status = 'PENDING'),
		       count(*) FILTER (WHERE status IN ('APPROVED', 'CONSUMED')),
		       count(*) FILTER (WHERE status = 'REJECTED')
		FROM public.ai_approvals WHERE tenant_id = $1
	`).Scan(&summary.Agentic.PendingApprovals, &summary.Agentic.ApprovedActions, &summary.Agentic.RejectedActions)
	if err != nil {
		return nil, fmt.Errorf("load AI approval analytics: %w", err)
	}

	return summary, nil
}
