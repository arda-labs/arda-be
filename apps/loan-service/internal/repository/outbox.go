package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func enqueueDisbursementEvent(ctx context.Context, q repoTX, tenantID, id, status, contractCode, createdBy, updatedBy string) error {
	status = strings.ToUpper(strings.TrimSpace(status))
	switch status {
	case "APPROVED", "REJECTED", "FAILED":
	default:
		return nil
	}
	var subject, eventCode string
	switch status {
	case "APPROVED":
		subject, eventCode = "arda.loan.disbursement.approved.v1", "loan.disbursement.approved"
	case "REJECTED":
		subject, eventCode = "arda.loan.disbursement.rejected.v1", "loan.disbursement.rejected"
	case "FAILED":
		subject, eventCode = "arda.loan.disbursement.failed.v1", "loan.disbursement.failed"
	}
	dedupe := "disbursement:" + id + ":" + strings.ToLower(status)
	users := make([]string, 0, 2)
	if createdBy != "" {
		users = append(users, createdBy)
	}
	if updatedBy != "" && updatedBy != createdBy {
		users = append(users, updatedBy)
	}
	payload, err := json.Marshal(map[string]any{
		"event_id": dedupe, "tenant_id": tenantID, "disbursement_id": id,
		"contract_code": contractCode, "status": status,
		"href": "/loan/disbursements/" + id, "locale": "vi-VN", "user_ids": users,
		"group_ids": []string{}, "role_codes": []string{}, "org_unit_ids": []string{}, "include_descendants": false,
	})
	if err != nil {
		return fmt.Errorf("encode disbursement event: %w", err)
	}
	_, err = q.ExecContext(ctx, `INSERT INTO loan_outbox (id, subject, event_code, dedupe_key, payload) VALUES ($1,$2,$3,$1,$4::jsonb) ON CONFLICT (dedupe_key) DO NOTHING`, dedupe, subject, eventCode, payload)
	if err != nil {
		return fmt.Errorf("write disbursement outbox: %w", err)
	}
	return nil
}
