package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Decision statuses: RECORDED means the workflow has durably accepted the
// decision; APPLIED means the business side effect (domain transition or
// posting) confirmed. Workers that handle approve/reject mark decisions
// APPLIED via FinishCase; the dispatcher handles return/submit transitions
// that have no service task on the BPMN path.
const (
	DecisionStatusRecorded = "RECORDED"
	DecisionStatusApplied  = "APPLIED"
	DecisionStatusFailed   = "FAILED"
)

// Dispatchable decisions are the ones with no worker on the BPMN path.
var dispatchableDecisions = []string{"REQUEST_CHANGES", "SUBMIT"}

// TaskDecision is one recorded human decision on a task activation.
type TaskDecision struct {
	ID                 string
	TaskID             string
	CaseID             string
	ProcessInstanceKey int64
	ElementID          string
	Decision           string
	Comment            string
	Actor              string
	DataVersion        string
	Status             string
	IdempotencyKey     string
	RecordedAt         time.Time
	DispatchedAt       *time.Time
	AppliedAt          *time.Time
	LastError          string

	// Joined case fields for the dispatcher.
	CaseType          string
	PrimaryObjectType string
	PrimaryObjectID   string
}

// InsertTaskDecision records a decision idempotently: replaying the same
// idempotency key returns the existing row instead of creating a duplicate.
func (r *CaseRepository) InsertTaskDecision(ctx context.Context, in TaskDecision) (*TaskDecision, error) {
	if strings.TrimSpace(in.TaskID) == "" || strings.TrimSpace(in.CaseID) == "" || strings.TrimSpace(in.Decision) == "" {
		return nil, errors.New("taskId, caseId and decision are required")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	var recorded TaskDecision
	var dispatchedAt, appliedAt sql.NullTime
	var lastError sql.NullString
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO workflow_task_decisions (
			id, task_id, case_id, process_instance_key, element_id, decision, comment,
			actor, data_version, status, idempotency_key
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, task_id, case_id, process_instance_key, element_id, decision, comment,
		          actor, COALESCE(data_version, ''), status, idempotency_key,
		          recorded_at, dispatched_at, applied_at, COALESCE(last_error, '')
	`, id, in.TaskID, in.CaseID, in.ProcessInstanceKey, in.ElementID, in.Decision,
		in.Comment, in.Actor, in.DataVersion, DecisionStatusRecorded, in.IdempotencyKey).
		Scan(&recorded.ID, &recorded.TaskID, &recorded.CaseID, &recorded.ProcessInstanceKey,
			&recorded.ElementID, &recorded.Decision, &recorded.Comment, &recorded.Actor,
			&recorded.DataVersion, &recorded.Status, &recorded.IdempotencyKey,
			&recorded.RecordedAt, &dispatchedAt, &appliedAt, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		existing, err := r.findDecisionByKey(ctx, in.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, errors.New("decision insert conflicted but no row was found")
		}
		return existing, nil
	}
	if err != nil {
		return nil, err
	}
	recorded.DispatchedAt = nullTimePtr(dispatchedAt)
	recorded.AppliedAt = nullTimePtr(appliedAt)
	recorded.LastError = lastError.String
	return &recorded, nil
}

func (r *CaseRepository) findDecisionByKey(ctx context.Context, key string) (*TaskDecision, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, task_id, case_id, process_instance_key, element_id, decision, comment,
		       actor, COALESCE(data_version, ''), status, idempotency_key,
		       recorded_at, dispatched_at, applied_at, COALESCE(last_error, '')
		FROM workflow_task_decisions
		WHERE idempotency_key = $1
	`, key)
	return scanTaskDecision(row)
}

// ListDispatchCandidates returns recorded/failed decisions that have no
// worker on the BPMN path, including the case coordinates the domain adapter
// needs. Failed rows are retried with a fixed cooldown.
func (r *CaseRepository) ListDispatchCandidates(ctx context.Context, limit int) ([]TaskDecision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id, d.task_id, d.case_id, d.process_instance_key, d.element_id, d.decision,
		       d.comment, d.actor, COALESCE(d.data_version, ''), d.status, d.idempotency_key,
		       d.recorded_at, d.dispatched_at, d.applied_at, COALESCE(d.last_error, ''),
		       bc.case_type, bc.primary_object_type, bc.primary_object_id
		FROM workflow_task_decisions d
		JOIN business_cases bc ON bc.id = d.case_id
		WHERE d.status IN ('RECORDED', 'FAILED')
		  AND d.decision = ANY($1)
		  AND (d.dispatched_at IS NULL OR d.dispatched_at < CURRENT_TIMESTAMP - INTERVAL '30 seconds')
		ORDER BY d.recorded_at
		LIMIT $2
	`, dispatchableDecisions, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TaskDecision, 0)
	for rows.Next() {
		var d TaskDecision
		var dispatchedAt, appliedAt sql.NullTime
		var lastError sql.NullString
		if err := rows.Scan(&d.ID, &d.TaskID, &d.CaseID, &d.ProcessInstanceKey, &d.ElementID,
			&d.Decision, &d.Comment, &d.Actor, &d.DataVersion, &d.Status, &d.IdempotencyKey,
			&d.RecordedAt, &dispatchedAt, &appliedAt, &lastError,
			&d.CaseType, &d.PrimaryObjectType, &d.PrimaryObjectID); err != nil {
			return nil, err
		}
		d.DispatchedAt = nullTimePtr(dispatchedAt)
		d.AppliedAt = nullTimePtr(appliedAt)
		d.LastError = lastError.String
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *CaseRepository) MarkDecisionApplied(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE workflow_task_decisions
		SET status = $2, applied_at = CURRENT_TIMESTAMP, dispatched_at = COALESCE(dispatched_at, CURRENT_TIMESTAMP),
		    last_error = NULL
		WHERE id = $1
	`, id, DecisionStatusApplied)
	return err
}

func (r *CaseRepository) MarkDecisionFailed(ctx context.Context, id, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE workflow_task_decisions
		SET status = $2, dispatched_at = CURRENT_TIMESTAMP, last_error = $3
		WHERE id = $1
	`, id, DecisionStatusFailed, truncate(reason, 500))
	return err
}

func scanTaskDecision(s scanner) (*TaskDecision, error) {
	var d TaskDecision
	var dispatchedAt, appliedAt sql.NullTime
	var lastError sql.NullString
	err := s.Scan(&d.ID, &d.TaskID, &d.CaseID, &d.ProcessInstanceKey, &d.ElementID,
		&d.Decision, &d.Comment, &d.Actor, &d.DataVersion, &d.Status, &d.IdempotencyKey,
		&d.RecordedAt, &dispatchedAt, &appliedAt, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.DispatchedAt = nullTimePtr(dispatchedAt)
	d.AppliedAt = nullTimePtr(appliedAt)
	d.LastError = lastError.String
	return &d, nil
}

func nullTimePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
