package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func enqueueWorkflowEvent(ctx context.Context, tx *sql.Tx, subject, eventCode, dedupeKey string, payload any) error {
	id, err := newID()
	if err != nil {
		return fmt.Errorf("generate workflow event id: %w", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode workflow event: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO workflow_outbox (id, subject, event_code, dedupe_key, payload)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (dedupe_key) DO NOTHING
	`, id, subject, eventCode, dedupeKey, encoded)
	if err != nil {
		return fmt.Errorf("write workflow outbox: %w", err)
	}
	return nil
}

func taskEventPayload(eventID, tenantID, taskID, taskType, title, href string, users, groups, roles, orgUnits []string) map[string]any {
	return map[string]any{
		"event_id": eventID, "tenant_id": tenantID, "task_id": taskID,
		"task_type": taskType, "title": title, "href": href, "locale": "vi-VN",
		"user_ids": users, "group_ids": groups, "role_codes": roles,
		"org_unit_ids": orgUnits, "include_descendants": false,
	}
}

func optionalString(value string) []string {
	if value = strings.TrimSpace(value); value == "" {
		return nil
	}
	return []string{value}
}
