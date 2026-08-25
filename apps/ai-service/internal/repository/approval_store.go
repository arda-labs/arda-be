package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrApprovalRunNotFound      = errors.New("AI approval run not found")
	ErrApprovalRunNotAwaiting   = errors.New("AI run cannot receive an approval")
	ErrApprovalIdempotencyMatch = errors.New("AI approval idempotency key already used with different data")
	ErrApprovalNotFound         = errors.New("AI approval not found")
	ErrApprovalExpired          = errors.New("AI approval expired")
	ErrApprovalState            = errors.New("AI approval is no longer pending")
	ErrApprovalSelf             = errors.New("requester cannot approve their own AI proposal")
)

type ApprovalProposal struct {
	Run               RunContext
	ToolName          string
	ToolVersion       int
	Risk              string
	ArgumentsRedacted string
	SummaryRedacted   string
	ResourceVersion   string
	PermissionVersion string
	ExpiresAt         time.Time
	IdempotencyKey    string
}

type ApprovalRecord struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
	Replayed  bool      `json:"replayed,omitempty"`
}

type ApprovalStore interface {
	CreateApprovalProposal(ctx context.Context, proposal ApprovalProposal) (ApprovalRecord, error)
	DecideApproval(ctx context.Context, tenantID, approvalID, approverUserID, decision string) (ApprovalRecord, error)
}

func (s *SQLRunStore) CreateApprovalProposal(ctx context.Context, proposal ApprovalProposal) (ApprovalRecord, error) {
	if s == nil || s.db == nil {
		return ApprovalRecord{}, fmt.Errorf("AI approval store is not configured")
	}
	if strings.TrimSpace(proposal.IdempotencyKey) == "" {
		return ApprovalRecord{}, fmt.Errorf("idempotency key is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, fmt.Errorf("begin AI approval transaction: %w", err)
	}
	defer tx.Rollback()

	var runID, runStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM ai.runs
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3
		FOR UPDATE
	`, proposal.Run.TenantID, proposal.Run.ActorUserID, proposal.Run.ExternalRun).Scan(&runID, &runStatus)
	if err == sql.ErrNoRows {
		return ApprovalRecord{}, ErrApprovalRunNotFound
	}
	if err != nil {
		return ApprovalRecord{}, fmt.Errorf("resolve AI approval run: %w", err)
	}
	if runStatus != "RUNNING" && runStatus != "WAITING_APPROVAL" {
		return ApprovalRecord{}, ErrApprovalRunNotAwaiting
	}

	var existing ApprovalRecord
	var existingArguments string
	err = tx.QueryRowContext(ctx, `
		SELECT a.id::text, a.status, a.expires_at, e.arguments_redacted::text
		FROM ai.approvals a
		JOIN ai.tool_executions e ON e.id = a.tool_execution_id
		WHERE a.tenant_id = $1 AND a.requester_user_id = $2 AND a.run_id = $3
		  AND e.idempotency_key = $4
		FOR UPDATE
	`, proposal.Run.TenantID, proposal.Run.ActorUserID, runID, proposal.IdempotencyKey).Scan(
		&existing.ID, &existing.Status, &existing.ExpiresAt, &existingArguments,
	)
	if err == nil {
		if !jsonEquivalent(existingArguments, proposal.ArgumentsRedacted) {
			return ApprovalRecord{}, ErrApprovalIdempotencyMatch
		}
		existing.Replayed = true
		if err := tx.Commit(); err != nil {
			return ApprovalRecord{}, fmt.Errorf("commit AI approval replay: %w", err)
		}
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return ApprovalRecord{}, fmt.Errorf("resolve AI approval replay: %w", err)
	}

	var executionID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ai.tool_executions
			(run_id, tenant_id, actor_user_id, tool_name, tool_version, risk, status,
			 arguments_redacted, policy_decision, idempotency_key, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'WAITING_APPROVAL', $7::jsonb,
			'allow_requires_approval', $8, now())
		RETURNING id::text
	`, runID, proposal.Run.TenantID, proposal.Run.ActorUserID, proposal.ToolName,
		fmt.Sprint(proposal.ToolVersion), proposal.Risk, proposal.ArgumentsRedacted,
		proposal.IdempotencyKey).Scan(&executionID)
	if err != nil {
		return ApprovalRecord{}, fmt.Errorf("persist AI approval tool execution: %w", err)
	}

	var record ApprovalRecord
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ai.approvals
			(run_id, tool_execution_id, tenant_id, requester_user_id, status,
			 summary_redacted, resource_version, permission_version, expires_at)
		VALUES ($1, $2, $3, $4, 'PENDING', $5::jsonb, NULLIF($6, ''), NULLIF($7, ''), $8)
		RETURNING id::text, status, expires_at
	`, runID, executionID, proposal.Run.TenantID, proposal.Run.ActorUserID,
		proposal.SummaryRedacted, proposal.ResourceVersion, proposal.PermissionVersion,
		proposal.ExpiresAt).Scan(&record.ID, &record.Status, &record.ExpiresAt)
	if err != nil {
		return ApprovalRecord{}, fmt.Errorf("persist AI approval: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ai.runs SET status = 'WAITING_APPROVAL' WHERE id = $1
	`, runID); err != nil {
		return ApprovalRecord{}, fmt.Errorf("pause AI run for approval: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, fmt.Errorf("commit AI approval: %w", err)
	}
	return record, nil
}

func jsonEquivalent(left, right string) bool {
	var leftValue, rightValue any
	if json.Unmarshal([]byte(left), &leftValue) != nil || json.Unmarshal([]byte(right), &rightValue) != nil {
		return left == right
	}
	leftJSON, leftErr := json.Marshal(leftValue)
	rightJSON, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func (s *SQLRunStore) DecideApproval(ctx context.Context, tenantID, approvalID, approverUserID, decision string) (ApprovalRecord, error) {
	if s == nil || s.db == nil {
		return ApprovalRecord{}, fmt.Errorf("AI approval store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, fmt.Errorf("begin AI approval decision: %w", err)
	}
	defer tx.Rollback()

	var requesterID, executionID, runID, status string
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT a.requester_user_id::text, a.tool_execution_id::text, a.run_id::text,
		       a.status, a.expires_at
		FROM ai.approvals a
		WHERE a.id = $1 AND a.tenant_id = $2
		FOR UPDATE
	`, approvalID, tenantID).Scan(&requesterID, &executionID, &runID, &status, &expiresAt)
	if err == sql.ErrNoRows {
		return ApprovalRecord{}, ErrApprovalNotFound
	}
	if err != nil {
		return ApprovalRecord{}, fmt.Errorf("resolve AI approval: %w", err)
	}
	if requesterID == approverUserID {
		return ApprovalRecord{}, ErrApprovalSelf
	}
	if status != "PENDING" {
		return ApprovalRecord{}, ErrApprovalState
	}
	if !expiresAt.After(time.Now().UTC()) {
		_, _ = tx.ExecContext(ctx, `UPDATE ai.approvals SET status = 'EXPIRED' WHERE id = $1`, approvalID)
		return ApprovalRecord{}, ErrApprovalExpired
	}
	if decision != "approve" && decision != "reject" {
		return ApprovalRecord{}, fmt.Errorf("decision must be approve or reject")
	}

	newStatus := "APPROVED"
	toolStatus := "WAITING_APPROVAL"
	if decision == "reject" {
		newStatus = "REJECTED"
		toolStatus = "DENIED"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ai.approvals
		SET status = $1, approver_user_id = $2
		WHERE id = $3 AND status = 'PENDING'
	`, newStatus, approverUserID, approvalID); err != nil {
		return ApprovalRecord{}, fmt.Errorf("decide AI approval: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ai.tool_executions SET status = $1 WHERE id = $2
	`, toolStatus, executionID); err != nil {
		return ApprovalRecord{}, fmt.Errorf("update AI approval tool state: %w", err)
	}
	if decision == "reject" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ai.runs SET status = 'FAILED', finished_at = now() WHERE id = $1
		`, runID); err != nil {
			return ApprovalRecord{}, fmt.Errorf("finish rejected AI run: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, fmt.Errorf("commit AI approval decision: %w", err)
	}
	return ApprovalRecord{ID: approvalID, Status: newStatus, ExpiresAt: expiresAt}, nil
}
