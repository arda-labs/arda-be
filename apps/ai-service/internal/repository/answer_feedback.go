package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AnswerFeedbackStore records a thumbs up/down on an assistant answer. Ratings
// are stored per (tenant, actor, conversation) so a later evaluation job can
// join them back to the answer text and the model that produced it.
type AnswerFeedbackStore interface {
	SaveAnswerFeedback(ctx context.Context, tenantID, actorUserID, threadID string, helpful bool, comment string) error
}

func (s *SQLRunStore) SaveAnswerFeedback(ctx context.Context, tenantID, actorUserID, threadID string, helpful bool, comment string) error {
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
	rating := 1
	reason := "not_helpful"
	if helpful {
		rating = 5
		reason = "helpful"
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO public.ai_feedback (tenant_id, actor_user_id, conversation_id, rating, reason, comment)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
	`, tenantID, actorUserID, conversationID, rating, reason, comment); err != nil {
		return fmt.Errorf("insert AI answer feedback: %w", err)
	}
	return nil
}
