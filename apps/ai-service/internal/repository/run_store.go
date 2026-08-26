package repository

import (
	"context"
	"database/sql"
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

type UsageSetter interface {
	SetUsage(ctx context.Context, run RunContext, usageJSON string) error
}

type ConversationSummary struct {
	ThreadID      string `json:"threadId"`
	Title         string `json:"title"`
	MessageCount  int    `json:"messageCount"`
	LastMessageAt string `json:"lastMessageAt,omitempty"`
	Status        string `json:"status"`
}

type ConversationMessage struct {
	Sequence  int64  `json:"sequence"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

type ConversationReader interface {
	ListConversations(ctx context.Context, tenantID, actorUserID string, limit int) ([]ConversationSummary, error)
	ConversationMessages(ctx context.Context, tenantID, actorUserID, threadID string, limit int) ([]ConversationMessage, error)
}

type ConversationMutator interface {
	DeleteConversation(ctx context.Context, tenantID, actorUserID, threadID string) error
}

type SQLRunStore struct {
	db *sql.DB
}

func NewSQLRunStore(db *sql.DB) *SQLRunStore {
	return &SQLRunStore{db: db}
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
		if len(title) > 80 {
			title = title[:80]
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
	`, run.TenantID, run.ActorUserID, run.ExternalRun, toolName, toolVersion, risk, jsonObject(argumentsRedacted), policyDecision).Scan(&executionID)
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
		SELECT m.sequence, m.role, m.content, m.created_at::text
		FROM public.ai_messages m
		JOIN public.ai_conversations c ON c.id = m.conversation_id
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
		if err := rows.Scan(&item.Sequence, &item.Role, &item.Content, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan AI message: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
		UPDATE public.ai_conversations SET status = 'DELETED', updated_at = now()
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

