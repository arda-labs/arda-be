package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const defaultConversationRetentionMonths = 1

func (s *SQLRunStore) GetConversationRetention(ctx context.Context, tenantID string) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("AI run store is not configured")
	}
	var months int
	err := s.db.QueryRowContext(ctx, `SELECT trash_retention_months
		FROM public.ai_conversation_settings WHERE tenant_id = $1`, tenantID).Scan(&months)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultConversationRetentionMonths, nil
	}
	return months, err
}

func (s *SQLRunStore) SaveConversationRetention(ctx context.Context, tenantID string, months int) error {
	if s == nil || s.db == nil {
		return errors.New("AI run store is not configured")
	}
	if tenantID == "" || months < 1 || months > 12 {
		return errors.New("invalid conversation trash retention")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO public.ai_conversation_settings
		(tenant_id, trash_retention_months) VALUES ($1::varchar(64), $2)
		ON CONFLICT (tenant_id) DO UPDATE SET trash_retention_months = EXCLUDED.trash_retention_months,
		updated_at = now()`, tenantID, months)
	return err
}

func (s *SQLRunStore) PermanentlyDeleteConversation(ctx context.Context, tenantID, actorUserID, threadID string) error {
	if s == nil || s.db == nil {
		return errors.New("AI run store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var conversationID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM public.ai_conversations
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_thread_id = $3
		AND status = 'DELETED' FOR UPDATE`, tenantID, actorUserID, threadID).Scan(&conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConversationNotFound
	}
	if err != nil {
		return err
	}
	if err := deleteConversationData(ctx, tx, conversationID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLRunStore) PermanentlyDeleteAllDeletedConversations(ctx context.Context, tenantID, actorUserID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("AI run store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM public.ai_conversations
		WHERE tenant_id = $1 AND actor_user_id = $2 AND status = 'DELETED' FOR UPDATE`, tenantID, actorUserID)
	if err != nil {
		return 0, err
	}
	ids, err := scanConversationIDs(rows)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := deleteConversationData(ctx, tx, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

// PurgeExpiredConversations is called periodically. Each batch uses row locks
// so a concurrent restore cannot race a retention purge.
func (s *SQLRunStore) PurgeExpiredConversations(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("AI run store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.id::text FROM public.ai_conversations c
		LEFT JOIN public.ai_conversation_settings s ON s.tenant_id = c.tenant_id
		WHERE c.status = 'DELETED' AND c.deleted_at IS NOT NULL
		AND c.deleted_at + make_interval(months => COALESCE(s.trash_retention_months, 1)) <= now()
		ORDER BY c.deleted_at LIMIT 500 FOR UPDATE OF c SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	ids, err := scanConversationIDs(rows)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := deleteConversationData(ctx, tx, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

func scanConversationIDs(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func deleteConversationData(ctx context.Context, tx *sql.Tx, conversationID string) error {
	statements := []struct {
		name string
		sql  string
	}{
		{"feedback", `DELETE FROM public.ai_feedback WHERE conversation_id = $1::uuid`},
		{"approvals", `DELETE FROM public.ai_approvals WHERE run_id IN (SELECT id FROM public.ai_runs WHERE conversation_id = $1::uuid)`},
		{"tool executions", `DELETE FROM public.ai_tool_executions WHERE run_id IN (SELECT id FROM public.ai_runs WHERE conversation_id = $1::uuid)`},
		{"messages", `DELETE FROM public.ai_messages WHERE conversation_id = $1::uuid`},
		{"runs", `DELETE FROM public.ai_runs WHERE conversation_id = $1::uuid`},
		{"conversation", `DELETE FROM public.ai_conversations WHERE id = $1::uuid`},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.sql, conversationID); err != nil {
			return fmt.Errorf("delete AI conversation %s: %w", statement.name, err)
		}
	}
	return nil
}
