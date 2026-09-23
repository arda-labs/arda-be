package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AnswerFeedbackStore records a thumbs up/down on an assistant answer. Ratings
// are stored per (tenant, actor, conversation) and, when the caller knows the
// run, linked to the exact assistant message so a later evaluation job can join
// them back to the answer text and the model that produced it.
type AnswerFeedbackStore interface {
	SaveAnswerFeedback(ctx context.Context, tenantID, actorUserID, threadID, runID string, helpful bool, comment string) error
}

func (s *SQLRunStore) SaveAnswerFeedback(ctx context.Context, tenantID, actorUserID, threadID, runID string, helpful bool, comment string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	var conversationID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id::text FROM public.ai_conversations
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_thread_id = $3 AND status = 'ACTIVE'
	`, tenantID, actorUserID, threadID).Scan(&conversationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConversationNotFound
		}
		return fmt.Errorf("resolve AI conversation for feedback: %w", err)
	}

	// Best-effort precise linkage: resolve the AG-UI run to the assistant
	// message it produced. Missing/unknown run ids leave message_id NULL so the
	// feedback is still recorded at the conversation level.
	var messageID sql.NullString
	if runID != "" {
		_ = s.db.QueryRowContext(ctx, `
			SELECT m.id::text
			FROM public.ai_messages m
			JOIN public.ai_runs r ON r.id = m.run_id
			WHERE r.tenant_id = $1 AND r.actor_user_id = $2
			  AND r.external_run_id = $3 AND r.conversation_id = $4
			  AND m.role = 'assistant'
			ORDER BY m.sequence DESC
			LIMIT 1
		`, tenantID, actorUserID, runID, conversationID).Scan(&messageID)
	}

	rating := 1
	reason := "not_helpful"
	if helpful {
		rating = 5
		reason = "helpful"
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO public.ai_feedback (tenant_id, actor_user_id, conversation_id, message_id, rating, reason, comment)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))
	`, tenantID, actorUserID, conversationID, messageID, rating, reason, comment); err != nil {
		return fmt.Errorf("insert AI answer feedback: %w", err)
	}
	return nil
}
