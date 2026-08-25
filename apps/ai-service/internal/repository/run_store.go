package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrRunAlreadyExists = errors.New("AI run already exists")

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
		FROM ai.conversations
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_thread_id = $3
		FOR UPDATE
	`, run.TenantID, run.ActorUserID, run.ExternalThread).Scan(&conversationID)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO ai.conversations (tenant_id, actor_user_id, external_thread_id, last_message_at)
			VALUES ($1, $2, $3, now())
			RETURNING id::text
		`, run.TenantID, run.ActorUserID, run.ExternalThread).Scan(&conversationID)
	}
	if err != nil {
		return fmt.Errorf("resolve AI conversation: %w", err)
	}

	var internalRunID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ai.runs
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
		UPDATE ai.conversations SET updated_at = now(), last_message_at = now() WHERE id = $1
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
		FROM ai.runs
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
		FOR UPDATE
	`, run.TenantID, run.ActorUserID, run.ExternalRun).Scan(&internalRunID, &conversationID); err != nil {
		return fmt.Errorf("resolve AI run for finish: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ai.runs
		SET status = $1, finished_at = now(), last_event_sequence = last_event_sequence + 1
		WHERE id = $2
	`, status, internalRunID); err != nil {
		return fmt.Errorf("finish AI run: %w", err)
	}
	if err := insertMessage(ctx, tx, conversationID, internalRunID, "assistant", assistantMessage); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ai.conversations SET updated_at = now(), last_message_at = now() WHERE id = $1
	`, conversationID); err != nil {
		return fmt.Errorf("update AI conversation after finish: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit AI run finish: %w", err)
	}
	return nil
}

func insertMessage(ctx context.Context, tx *sql.Tx, conversationID, runID, role, content string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ai.messages (conversation_id, run_id, sequence, role, content)
		SELECT $1, $2, COALESCE(MAX(sequence), 0) + 1, $3, $4
		FROM ai.messages
		WHERE conversation_id = $1
	`, conversationID, runID, role, content); err != nil {
		return fmt.Errorf("persist AI %s message: %w", role, err)
	}
	return nil
}
