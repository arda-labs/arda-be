package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	TaskStatusReady     = "READY"
	TaskStatusClaimed   = "CLAIMED"
	TaskStatusCompleted = "COMPLETED"
	TaskStatusCancelled = "CANCELLED"
)

type WorkItem struct {
	ID                       string     `json:"id"`
	CaseID                   string     `json:"caseId"`
	CaseCode                 string     `json:"caseCode"`
	CaseType                 string     `json:"caseType"`
	Direction                string     `json:"direction"`
	PrimaryObjectType        string     `json:"primaryObjectType"`
	PrimaryObjectID          string     `json:"primaryObjectId"`
	ProcessInstanceKey       *int64     `json:"processInstanceKey,omitempty"`
	JobKey                   *int64     `json:"jobKey,omitempty"`
	TaskType                 string     `json:"taskType"`
	StepCode                 string     `json:"stepCode"`
	Title                    string     `json:"title"`
	Description              string     `json:"description"`
	Summary                  string     `json:"summary"`
	Status                   string     `json:"status"`
	TransactionStatus        string     `json:"transactionStatus"`
	CreatedBy                string     `json:"createdBy"`
	CreatedByName            string     `json:"createdByName,omitempty"`
	CreatedByAvatar          string     `json:"createdByAvatar,omitempty"`
	CandidateRole            string     `json:"candidateRole"`
	CandidateGroupID         string     `json:"candidateGroupId,omitempty"`
	CandidateOrgUnitID       string     `json:"candidateOrgUnitId,omitempty"`
	AssignedTo               string     `json:"assignedTo,omitempty"`
	AssignedToName           string     `json:"assignedToName,omitempty"`
	AssignedToAvatar         string     `json:"assignedToAvatar,omitempty"`
	PreviousAssignedTo       string     `json:"previousAssignedTo,omitempty"`
	PreviousAssignedToName   string     `json:"previousAssignedToName,omitempty"`
	PreviousAssignedToAvatar string     `json:"previousAssignedToAvatar,omitempty"`
	AssignedAt               *time.Time `json:"assignedAt,omitempty"`
	ClaimExpiresAt           *time.Time `json:"claimExpiresAt,omitempty"`
	SLADueAt                 *time.Time `json:"slaDueAt,omitempty"`
	SLAStatus                string     `json:"slaStatus"`
	CanClaim                 bool       `json:"canClaim"`
	CanOpen                  bool       `json:"canOpen"`
	CanReassign              bool       `json:"canReassign"`
	ClaimBlockedReason       string     `json:"claimBlockedReason,omitempty"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

func (item WorkItem) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"id":                       item.ID,
		"caseId":                   item.CaseID,
		"caseCode":                 item.CaseCode,
		"caseType":                 item.CaseType,
		"direction":                item.Direction,
		"primaryObjectType":        item.PrimaryObjectType,
		"primaryObjectId":          item.PrimaryObjectID,
		"taskType":                 item.TaskType,
		"stepCode":                 item.StepCode,
		"title":                    item.Title,
		"description":              item.Description,
		"summary":                  item.Summary,
		"status":                   item.Status,
		"transactionStatus":        item.TransactionStatus,
		"createdBy":                item.CreatedBy,
		"createdByName":            item.CreatedByName,
		"createdByAvatar":          item.CreatedByAvatar,
		"candidateRole":            item.CandidateRole,
		"candidateGroupId":         item.CandidateGroupID,
		"candidateOrgUnitId":       item.CandidateOrgUnitID,
		"assignedTo":               item.AssignedTo,
		"assignedToName":           item.AssignedToName,
		"assignedToAvatar":         item.AssignedToAvatar,
		"previousAssignedTo":       item.PreviousAssignedTo,
		"previousAssignedToName":   item.PreviousAssignedToName,
		"previousAssignedToAvatar": item.PreviousAssignedToAvatar,
		"assignedAt":               item.AssignedAt,
		"claimExpiresAt":           item.ClaimExpiresAt,
		"slaDueAt":                 item.SLADueAt,
		"slaStatus":                item.SLAStatus,
		"canClaim":                 item.CanClaim,
		"canOpen":                  item.CanOpen,
		"canReassign":              item.CanReassign,
		"claimBlockedReason":       item.ClaimBlockedReason,
		"createdAt":                item.CreatedAt,
		"updatedAt":                item.UpdatedAt,
	}
	if item.ProcessInstanceKey != nil {
		payload["processInstanceKey"] = strconv.FormatInt(*item.ProcessInstanceKey, 10)
	}
	if item.JobKey != nil {
		payload["jobKey"] = strconv.FormatInt(*item.JobKey, 10)
	}
	return json.Marshal(payload)
}

type WorkItemFilter struct {
	Direction         string
	From              *time.Time
	To                *time.Time
	Accounting        string
	SLAStatus         string
	TransactionStatus string
	Node              string
	UserID            string
	CreatedBy         string
	Limit             int
}

type WorkItemSummaryNode struct {
	ID       string                `json:"id"`
	Label    string                `json:"label"`
	Count    int                   `json:"count"`
	Overdue  int                   `json:"overdue"`
	Children []WorkItemSummaryNode `json:"children,omitempty"`
}

type WorkItemSeed struct {
	CaseID             string
	ProcessInstanceKey *int64
	JobKey             *int64
	TaskType           string
	StepCode           string
	CandidateRole      string
	CandidateGroupID   string
	CandidateOrgUnitID string
	SLADueAt           *time.Time
	Title              string
	Description        string
}

func (r *CaseRepository) UpsertWorkItem(ctx context.Context, seed WorkItemSeed) (*WorkItem, error) {
	if seed.CaseID == "" || seed.TaskType == "" || seed.StepCode == "" {
		return nil, errors.New("caseId, taskType and stepCode are required")
	}
	if seed.SLADueAt == nil {
		dueAt, err := r.workItemSLADueAt(ctx, seed.CaseID, seed.StepCode)
		if err != nil {
			return nil, err
		}
		seed.SLADueAt = dueAt
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO workflow_tasks (
			id, case_id, process_instance_key, job_key, task_type, step_code,
			title, description, status, candidate_role, candidate_group_id,
			candidate_org_unit_id, sla_due_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (case_id, task_type, step_code) DO UPDATE SET
			process_instance_key = COALESCE(EXCLUDED.process_instance_key, workflow_tasks.process_instance_key),
			job_key = COALESCE(EXCLUDED.job_key, workflow_tasks.job_key),
			title = COALESCE(NULLIF(EXCLUDED.title, ''), workflow_tasks.title),
			description = COALESCE(NULLIF(EXCLUDED.description, ''), workflow_tasks.description),
			candidate_role = COALESCE(NULLIF(EXCLUDED.candidate_role, ''), workflow_tasks.candidate_role),
			candidate_group_id = COALESCE(NULLIF(EXCLUDED.candidate_group_id, ''), workflow_tasks.candidate_group_id),
			candidate_org_unit_id = COALESCE(NULLIF(EXCLUDED.candidate_org_unit_id, ''), workflow_tasks.candidate_org_unit_id),
			status = CASE
				WHEN workflow_tasks.status = 'CLAIMED' THEN workflow_tasks.status
				WHEN workflow_tasks.status IN ('COMPLETED', 'CANCELLED')
					AND EXCLUDED.job_key IS NOT NULL
					AND workflow_tasks.job_key IS DISTINCT FROM EXCLUDED.job_key THEN 'READY'
				ELSE workflow_tasks.status
			END,
			assigned_to = CASE
				WHEN workflow_tasks.status = 'CLAIMED' THEN workflow_tasks.assigned_to
				WHEN EXCLUDED.job_key IS NOT NULL
					AND workflow_tasks.job_key IS DISTINCT FROM EXCLUDED.job_key THEN ''
				ELSE workflow_tasks.assigned_to
			END,
			assigned_at = CASE
				WHEN workflow_tasks.status = 'CLAIMED' THEN workflow_tasks.assigned_at
				WHEN EXCLUDED.job_key IS NOT NULL
					AND workflow_tasks.job_key IS DISTINCT FROM EXCLUDED.job_key THEN NULL
				ELSE workflow_tasks.assigned_at
			END,
			claim_expires_at = CASE
				WHEN workflow_tasks.status = 'CLAIMED' THEN workflow_tasks.claim_expires_at
				WHEN EXCLUDED.job_key IS NOT NULL
					AND workflow_tasks.job_key IS DISTINCT FROM EXCLUDED.job_key THEN NULL
				ELSE workflow_tasks.claim_expires_at
			END,
			sla_due_at = COALESCE(EXCLUDED.sla_due_at, workflow_tasks.sla_due_at),
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`, id, seed.CaseID, seed.ProcessInstanceKey, seed.JobKey, seed.TaskType, seed.StepCode,
		seed.Title, seed.Description, TaskStatusReady, seed.CandidateRole, seed.CandidateGroupID,
		seed.CandidateOrgUnitID, seed.SLADueAt)
	var workItemID string
	if err := row.Scan(&workItemID); err != nil {
		return nil, err
	}
	return r.GetWorkItem(ctx, workItemID, "")
}

func (r *CaseRepository) workItemSLADueAt(ctx context.Context, caseID string, stepCode string) (*time.Time, error) {
	var dueAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(
			(
				SELECT CASE stp.duration_unit
					WHEN 'MINUTE' THEN CURRENT_TIMESTAMP + (stp.duration_value * INTERVAL '1 minute')
					WHEN 'HOUR' THEN CURRENT_TIMESTAMP + (stp.duration_value * INTERVAL '1 hour')
				END
				FROM business_cases bc
				JOIN business_sla_task_policies stp ON stp.sla_policy_id = bc.sla_policy_id
				WHERE bc.id = $1
				  AND stp.step_code = $2
				  AND stp.status = 'ACTIVE'
				  AND stp.effective_from <= CURRENT_TIMESTAMP
				  AND (stp.effective_to IS NULL OR stp.effective_to > CURRENT_TIMESTAMP)
				ORDER BY stp.sort_order
				LIMIT 1
			),
			(
				SELECT CURRENT_TIMESTAMP + (sp.due_in_hours * INTERVAL '1 hour')
				FROM business_cases bc
				JOIN business_sla_policies sp ON sp.id = bc.sla_policy_id
				WHERE bc.id = $1
				  AND sp.status = 'ACTIVE'
				  AND sp.effective_from <= CURRENT_TIMESTAMP
				  AND (sp.effective_to IS NULL OR sp.effective_to > CURRENT_TIMESTAMP)
				LIMIT 1
			)
		)
	`, caseID, stepCode).Scan(&dueAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !dueAt.Valid {
		return nil, nil
	}
	return &dueAt.Time, nil
}

func (r *CaseRepository) GetWorkItem(ctx context.Context, id string, userID string) (*WorkItem, error) {
	row := r.db.QueryRowContext(ctx, workItemSelectSQL()+` WHERE wt.id = $1`, id)
	item, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.decorate(userID, "")
	return &item, nil
}

func (r *CaseRepository) FindActiveWorkTask(ctx context.Context, caseID string, processInstanceKey int64) (*WorkItem, error) {
	if caseID == "" && processInstanceKey <= 0 {
		return nil, nil
	}
	where := []string{
		"wt.status IN ('READY', 'CLAIMED')",
		"wt.job_key IS NOT NULL",
	}
	args := []any{}
	if caseID != "" {
		args = append(args, caseID)
		where = append(where, fmt.Sprintf("wt.case_id = $%d", len(args)))
	}
	if processInstanceKey > 0 {
		args = append(args, processInstanceKey)
		where = append(where, fmt.Sprintf("bc.process_instance_key = $%d", len(args)))
	}
	row := r.db.QueryRowContext(ctx, workItemSelectSQL()+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY wt.updated_at DESC
		LIMIT 1
	`, args...)
	item, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.decorate("", "INCOMING")
	return &item, nil
}

func (r *CaseRepository) ListWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error) {
	var items []WorkItem
	var err error
	switch strings.ToUpper(strings.TrimSpace(f.Direction)) {
	case "OUTGOING":
		items, err = r.listOutgoingWorkItems(ctx, f)
	case "ALL":
		items, err = r.listSearchWorkItems(ctx, f)
	default:
		items, err = r.listIncomingWorkItems(ctx, f)
	}
	if err != nil {
		return nil, err
	}
	r.enrichWorkItemUsers(ctx, items)
	return items, nil
}

func (r *CaseRepository) listIncomingWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error) {
	where := []string{
		"wt.status IN ('READY', 'CLAIMED')",
		"bc.status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')",
	}
	return r.queryWorkItems(ctx, f, where, "INCOMING", false)
}

func (r *CaseRepository) listOutgoingWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error) {
	if strings.TrimSpace(f.UserID) == "" {
		return nil, nil
	}
	outFilter := f
	outFilter.CreatedBy = f.UserID
	// Theo dõi hồ sơ mình đã gửi: task hiện tại của case (distinct mới nhất).
	// Loại UT_MakerRevise đang READY/CLAIMED — đó là việc inbox ở Giao dịch đến
	// (bước đầu sau Khởi tạo / khi KS yêu cầu bổ sung), không phải giao dịch đi.
	where := []string{
		"wt.status IN ('READY', 'CLAIMED', 'COMPLETED')",
		"bc.status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')",
		`NOT (
			wt.status IN ('READY', 'CLAIMED')
			AND wt.step_code IN ('UT_MakerRevise', 'Activity_MakerRevise')
		)`,
	}
	return r.queryWorkItems(ctx, outFilter, where, "OUTGOING", true)
}

func (r *CaseRepository) listSearchWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error) {
	where := []string{"bc.status <> 'DRAFT'"}
	return r.queryWorkItems(ctx, f, where, "ALL", true)
}

func (r *CaseRepository) queryWorkItems(
	ctx context.Context,
	f WorkItemFilter,
	baseWhere []string,
	queueDirection string,
	distinctByCase bool,
) ([]WorkItem, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}

	where := append([]string{}, baseWhere...)
	args := []any{}
	add := func(sql string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(sql, len(args)))
	}
	if strings.TrimSpace(f.CreatedBy) != "" {
		add("bc.created_by = $%d", f.CreatedBy)
	}
	if f.To != nil {
		add("bc.created_at < $%d", f.To.AddDate(0, 0, 1))
	}
	if f.TransactionStatus != "" && f.TransactionStatus != "ALL" {
		add("bc.status = $%d", f.TransactionStatus)
	}
	if f.Node != "" && f.Node != "ALL" {
		args = append(args, f.Node, f.Node)
		n := len(args)
		where = append(where, fmt.Sprintf("(wt.step_code = $%d OR bc.current_step = $%d)", n-1, n))
	}
	if f.SLAStatus != "" && f.SLAStatus != "ALL" {
		switch f.SLAStatus {
		case "MET":
			where = append(where, "(COALESCE(wt.sla_due_at, bc.sla_due_at) IS NULL OR COALESCE(wt.sla_due_at, bc.sla_due_at) >= CURRENT_TIMESTAMP)")
		case "BREACHED":
			where = append(where, "COALESCE(wt.sla_due_at, bc.sla_due_at) < CURRENT_TIMESTAMP")
		}
	}
	if f.Accounting == "POSTED" {
		where = append(where, "bc.status = 'COMPLETED'")
	}
	if f.Accounting == "NOT_POSTED" {
		where = append(where, "bc.status <> 'COMPLETED'")
	}

	args = append(args, f.Limit)
	limitPos := len(args)
	sql := workItemSelectSQL() + `
		WHERE ` + strings.Join(where, " AND ")
	if distinctByCase {
		sql = `
			SELECT * FROM (
				SELECT DISTINCT ON (bc.id)
					wt.id, bc.id, bc.case_code, bc.case_type, bc.primary_object_type, bc.primary_object_id,
					bc.process_instance_key, wt.job_key, wt.task_type, wt.step_code,
					wt.title, wt.description, wt.status, bc.status, bc.created_by,
					wt.candidate_role, wt.candidate_group_id, wt.candidate_org_unit_id,
					wt.assigned_to, wt.assigned_at, wt.claim_expires_at,
					COALESCE((
						SELECT prev.assigned_to
						FROM workflow_tasks prev
						WHERE prev.case_id = wt.case_id
						  AND prev.id <> wt.id
						  AND prev.status = 'COMPLETED'
						  AND prev.assigned_to <> ''
						ORDER BY prev.updated_at DESC
						LIMIT 1
					), '') AS previous_assigned_to,
					COALESCE(wt.sla_due_at, bc.sla_due_at), wt.created_at, wt.updated_at
				FROM workflow_tasks wt
				JOIN business_cases bc ON bc.id = wt.case_id
				WHERE ` + strings.Join(where, " AND ") + `
				ORDER BY bc.id, wt.created_at DESC
			) latest
			ORDER BY latest.created_at DESC
			LIMIT $` + fmt.Sprint(limitPos)
	} else {
		sql += `
		ORDER BY wt.created_at DESC
		LIMIT $` + fmt.Sprint(limitPos)
	}

	rows, err := r.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WorkItem
	for rows.Next() {
		item, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		item.decorate(f.UserID, queueDirection)
		out = append(out, item)
	}
	return out, rows.Err()
}

func dedupeWorkItemsByCase(items []WorkItem) []WorkItem {
	seen := make(map[string]struct{}, len(items))
	out := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.CaseID]; ok {
			continue
		}
		seen[item.CaseID] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (r *CaseRepository) ClaimWorkItem(ctx context.Context, id string, actor string) (*WorkItem, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var assignedTo, status string
	err = tx.QueryRowContext(ctx, `
		SELECT assigned_to, status
		FROM workflow_tasks
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&assignedTo, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status == TaskStatusCompleted || status == TaskStatusCancelled {
		return nil, fmt.Errorf("task is %s", status)
	}
	if assignedTo != "" && assignedTo != actor {
		return nil, fmt.Errorf("task already claimed by %s", assignedTo)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE workflow_tasks
		SET status = $2, assigned_to = $3, assigned_at = COALESCE(assigned_at, CURRENT_TIMESTAMP),
		    claim_expires_at = COALESCE(claim_expires_at, CURRENT_TIMESTAMP + INTERVAL '30 minutes'),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id, TaskStatusClaimed, actor)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE business_cases
		SET assigned_to = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = (SELECT case_id FROM workflow_tasks WHERE id = $1)
	`, id, actor)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetWorkItem(ctx, id, actor)
}

func (r *CaseRepository) CompleteWorkItemByJob(ctx context.Context, jobKey int64) error {
	if jobKey == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE workflow_tasks
		SET status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE job_key = $1
	`, jobKey, TaskStatusCompleted)
	return err
}

func (r *CaseRepository) UserCanClaimRole(ctx context.Context, userID string, groupIDs []string, roleCode string) (bool, error) {
	if userID == "" || roleCode == "" {
		return false, nil
	}
	var ok bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM workflow_role_memberships
			WHERE role_code = $1
			  AND principal_type = 'USER'
			  AND principal_id = $2
			  AND status = 'ACTIVE'
			  AND effective_from <= CURRENT_TIMESTAMP
			  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP)
		)
	`, roleCode, userID).Scan(&ok)
	if err != nil || ok {
		return ok, err
	}

	// ponytail: group matching only checks principal id; add tenant/org/branch/amount constraints when task context carries those fields.
	for _, groupID := range groupIDs {
		if groupID == "" {
			continue
		}
		err = r.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM workflow_role_memberships
				WHERE role_code = $1
				  AND principal_type = 'GROUP'
				  AND principal_id = $2
				  AND status = 'ACTIVE'
				  AND effective_from <= CURRENT_TIMESTAMP
				  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP)
			)
		`, roleCode, groupID).Scan(&ok)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func workItemSelectSQL() string {
	return `
		SELECT
			wt.id, bc.id, bc.case_code, bc.case_type, bc.primary_object_type, bc.primary_object_id,
			bc.process_instance_key, wt.job_key, wt.task_type, wt.step_code,
			wt.title, wt.description, wt.status, bc.status, bc.created_by,
			wt.candidate_role, wt.candidate_group_id, wt.candidate_org_unit_id,
			wt.assigned_to, wt.assigned_at, wt.claim_expires_at,
			COALESCE((
				SELECT prev.assigned_to
				FROM workflow_tasks prev
				WHERE prev.case_id = wt.case_id
				  AND prev.id <> wt.id
				  AND prev.status = 'COMPLETED'
				  AND prev.assigned_to <> ''
				ORDER BY prev.updated_at DESC
				LIMIT 1
			), '') AS previous_assigned_to,
			COALESCE(wt.sla_due_at, bc.sla_due_at), wt.created_at, wt.updated_at
		FROM workflow_tasks wt
		JOIN business_cases bc ON bc.id = wt.case_id
	`
}

func scanWorkItem(s scanner) (WorkItem, error) {
	var item WorkItem
	var processInstanceKey, jobKey sql.NullInt64
	var assignedAt, claimExpiresAt, slaDueAt sql.NullTime
	err := s.Scan(
		&item.ID, &item.CaseID, &item.CaseCode, &item.CaseType, &item.PrimaryObjectType, &item.PrimaryObjectID,
		&processInstanceKey, &jobKey, &item.TaskType, &item.StepCode,
		&item.Title, &item.Description, &item.Status, &item.TransactionStatus, &item.CreatedBy,
		&item.CandidateRole, &item.CandidateGroupID, &item.CandidateOrgUnitID,
		&item.AssignedTo, &assignedAt, &claimExpiresAt,
		&item.PreviousAssignedTo,
		&slaDueAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if processInstanceKey.Valid {
		item.ProcessInstanceKey = &processInstanceKey.Int64
	}
	if jobKey.Valid {
		item.JobKey = &jobKey.Int64
	}
	if assignedAt.Valid {
		item.AssignedAt = &assignedAt.Time
	}
	if claimExpiresAt.Valid {
		item.ClaimExpiresAt = &claimExpiresAt.Time
	}
	if slaDueAt.Valid {
		item.SLADueAt = &slaDueAt.Time
	}
	return item, err
}

func (item *WorkItem) decorate(userID, queueDirection string) {
	item.Direction = directionForCaseType(item.CaseType)
	if queueDirection == "OUTGOING" && userID != "" && item.CreatedBy == userID && isMakerTrackCaseType(item.CaseType) {
		item.Direction = "OUTGOING"
	}
	item.SLAStatus = slaStatus(item.SLADueAt)
	if item.Title == "" {
		item.Title = taskTitle(item.TaskType, item.StepCode)
	}
	if item.Description == "" {
		item.Description = taskDescription(item.TaskType, item.CaseCode)
	}
	item.Summary = item.Description

	// Populate display name from user ID / email
	item.AssignedToName = displayName(item.AssignedTo)
	item.PreviousAssignedToName = displayName(item.PreviousAssignedTo)
	item.CreatedByName = displayName(item.CreatedBy)

	if queueDirection == "OUTGOING" && userID != "" && item.CreatedBy == userID && isMakerTrackCaseType(item.CaseType) {
		item.CanOpen = true
		item.CanClaim = false
		return
	}

	item.CanOpen = item.AssignedTo == "" || item.AssignedTo == userID
	item.CanClaim = item.Status == TaskStatusReady && item.AssignedTo == ""
	if item.AssignedTo != "" && item.AssignedTo != userID {
		item.ClaimBlockedReason = "Task đang được xử lý bởi " + item.AssignedTo
	}
	if item.Status == TaskStatusCompleted || item.Status == TaskStatusCancelled {
		item.CanClaim = false
		item.CanOpen = false
	}
}

func isMakerTrackCaseType(caseType string) bool {
	switch caseType {
	case "CUSTOMER_REGISTRATION", "CUSTOMER_ADJUSTMENT", "HRM_EMPLOYEE_REGISTRATION":
		return true
	default:
		return false
	}
}

// IsMakerTrackCaseType reports case types where the creator tracks progress
// on the outgoing workbench after submitting.
func IsMakerTrackCaseType(caseType string) bool {
	return isMakerTrackCaseType(caseType)
}

func displayName(id string) string {
	if id == "" {
		return ""
	}
	if idx := strings.Index(id, "@"); idx > 0 {
		return id[:idx]
	}
	return id
}

func (r *CaseRepository) enrichWorkItemUsers(ctx context.Context, items []WorkItem) {
	if r.iamClient == nil || len(items) == 0 {
		return
	}
	ids := make(map[string]struct{})
	for _, item := range items {
		if item.AssignedTo != "" {
			ids[item.AssignedTo] = struct{}{}
		}
		if item.CreatedBy != "" {
			ids[item.CreatedBy] = struct{}{}
		}
		if item.PreviousAssignedTo != "" {
			ids[item.PreviousAssignedTo] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return
	}
	userIDs := make([]string, 0, len(ids))
	for id := range ids {
		userIDs = append(userIDs, id)
	}
	users, err := r.iamClient.GetUserBatch(ctx, userIDs)
	if err != nil {
		return
	}
	for i, item := range items {
		if u, ok := users[item.AssignedTo]; ok {
			items[i].AssignedToName = u.Name
			items[i].AssignedToAvatar = u.AvatarURL
		}
		if u, ok := users[item.CreatedBy]; ok {
			items[i].CreatedByName = u.Name
			items[i].CreatedByAvatar = u.AvatarURL
		}
		if u, ok := users[item.PreviousAssignedTo]; ok {
			items[i].PreviousAssignedToName = u.Name
			items[i].PreviousAssignedToAvatar = u.AvatarURL
		}
	}
}

func directionForCaseType(caseType string) string {
	if caseType == "FINANCE_OUTGOING_TRANSACTION" {
		return "OUTGOING"
	}
	return "INCOMING"
}

func slaStatus(dueAt *time.Time) string {
	if dueAt == nil {
		return "NONE"
	}
	if dueAt.Before(time.Now()) {
		return "BREACHED"
	}
	return "MET"
}

func taskTitle(taskType string, stepCode string) string {
	labels := map[string]string{
		"workflow.customer_checker_review":   "Phê duyệt hồ sơ khách hàng",
		"workflow.customer_risk_review":      "Rà soát rủi ro khách hàng",
		"workflow.customer_maker_revise":     "Chỉnh sửa hồ sơ",
		"workflow.finance_incoming_classify": "Phân loại giao dịch đến",
		"workflow.finance_incoming_approve":  "Duyệt giao dịch đến",
		"workflow.finance_outgoing_verify":   "Kiểm tra giao dịch đi",
		"workflow.finance_outgoing_approve":  "Duyệt giao dịch đi",
	}
	if label, ok := labels[taskType]; ok {
		return label
	}
	if stepCode != "" {
		return stepCode
	}
	return taskType
}

func taskDescription(taskType string, caseCode string) string {
	subject := caseCode
	if subject == "" {
		subject = "hồ sơ"
	}
	switch taskType {
	case "workflow.customer_checker_review":
		return "Phê duyệt thông tin định danh, hồ sơ đính kèm và quyết định bước tiếp theo cho " + subject + "."
	case "workflow.customer_risk_review":
		return "Đánh giá mức rủi ro và đưa quyết định rủi ro cho " + subject + "."
	case "workflow.customer_maker_revise":
		return "Chỉnh sửa hồ sơ theo yêu cầu phê duyệt cho " + subject + "."
	case "workflow.finance_incoming_classify":
		return "Phân loại giao dịch đến và chuẩn bị thông tin hạch toán cho " + subject + "."
	case "workflow.finance_incoming_approve":
		return "Kiểm tra kết quả phân loại và phê duyệt ghi nhận giao dịch đến " + subject + "."
	case "workflow.finance_outgoing_verify":
		return "Kiểm tra người nhận và dữ liệu giao dịch đi trước khi trình duyệt " + subject + "."
	case "workflow.finance_outgoing_approve":
		return "Rà soát hạn mức và phê duyệt giao dịch đi " + subject + "."
	default:
		return "Xử lý bước " + taskTitle(taskType, "") + " cho " + subject + "."
	}
}
