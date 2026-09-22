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

	ardapg "github.com/arda-labs/arda/libs/go/arda-postgres"
)

const (
	TaskStatusRouting   = "ROUTING"
	TaskStatusReady     = "READY"
	TaskStatusClaimed   = "CLAIMED"
	TaskStatusCompleted = "COMPLETED"
	TaskStatusCancelled = "CANCELLED"
)

type WorkItem struct {
	ID                       string     `json:"id"`
	CaseID                   string     `json:"caseId"`
	TenantID                 string     `json:"-"`
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
	CandidateUsers           []string   `json:"candidateUsers,omitempty"`
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
	CanView                  bool       `json:"-"`
	ClaimBlockedReason       string     `json:"claimBlockedReason,omitempty"`
	// Registry v2 step metadata (decorated by the HTTP layer from
	// workflow_case_type_steps): the shared task UI renders actions/forms from
	// these fields instead of hardcoded case-type maps.
	StepKind          string    `json:"stepKind,omitempty"`
	FormKey           string    `json:"formKey,omitempty"`
	AllowedActions    []string  `json:"allowedActions,omitempty"`
	RequiredCommentOn []string  `json:"requiredCommentOn,omitempty"`
	RegistryVersion   int       `json:"registryVersion,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
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
		"stepKind":                 item.StepKind,
		"formKey":                  item.FormKey,
		"allowedActions":           item.AllowedActions,
		"requiredCommentOn":        item.RequiredCommentOn,
		"registryVersion":          item.RegistryVersion,
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
	Domain            string
	UserID            string
	CreatedBy         string
	AssignedTo        string
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
	CandidateUsers     []string
	CandidateGroupID   string
	CandidateOrgUnitID string
	SLADueAt           *time.Time
	Title              string
	Description        string
}

func workItemSeedStatus(seed WorkItemSeed) string {
	if seed.JobKey == nil {
		return TaskStatusRouting
	}
	return TaskStatusReady
}

// UpsertWorkItem writes one task activation. The eager ROUTING row created at
// submit is bound in place when the projector supplies the engine job key;
// every later activation of the same step (return loops, retries) inserts a
// new row and cancels the previous open one, so task history is preserved.
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

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		existingID     string
		existingStatus string
		existingJobKey sql.NullInt64
		activationNo   int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, status, job_key, activation_no
		FROM workflow_tasks
		WHERE case_id = $1 AND step_code = $2
		ORDER BY activation_no DESC
		LIMIT 1
		FOR UPDATE
	`, seed.CaseID, seed.StepCode).Scan(&existingID, &existingStatus, &existingJobKey, &activationNo)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		id, err := newID()
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO workflow_tasks (
				id, case_id, process_instance_key, job_key, task_type, step_code,
				title, description, status, candidate_role, candidate_users, candidate_group_id,
				candidate_org_unit_id, sla_due_at, activation_no
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1)
		`, id, seed.CaseID, seed.ProcessInstanceKey, seed.JobKey, seed.TaskType, seed.StepCode,
			seed.Title, seed.Description, workItemSeedStatus(seed), seed.CandidateRole,
			ardapg.Driver.NotNil(seed.CandidateUsers), seed.CandidateGroupID,
			seed.CandidateOrgUnitID, seed.SLADueAt); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return r.GetWorkItem(ctx, id, "")
	case err != nil:
		return nil, err
	}

	sameJobKey := seed.JobKey != nil && existingJobKey.Valid && *seed.JobKey == existingJobKey.Int64
	bindPlaceholder := seed.JobKey != nil && !existingJobKey.Valid && existingStatus == TaskStatusRouting

	if seed.JobKey != nil && !sameJobKey && !bindPlaceholder {
		// New engine activation for this step: close the previous open rows
		// and insert a fresh READY row so every activation keeps its history.
		if _, err := tx.ExecContext(ctx, `
			UPDATE workflow_tasks
			SET status = $3, updated_at = CURRENT_TIMESTAMP
			WHERE case_id = $1 AND step_code = $2 AND status IN ($4, $5, $6)
		`, seed.CaseID, seed.StepCode, TaskStatusCancelled,
			TaskStatusRouting, TaskStatusReady, TaskStatusClaimed); err != nil {
			return nil, err
		}
		id, err := newID()
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO workflow_tasks (
				id, case_id, process_instance_key, job_key, task_type, step_code,
				title, description, status, candidate_role, candidate_users, candidate_group_id,
				candidate_org_unit_id, sla_due_at, activation_no
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		`, id, seed.CaseID, seed.ProcessInstanceKey, seed.JobKey, seed.TaskType, seed.StepCode,
			seed.Title, seed.Description, TaskStatusReady, seed.CandidateRole,
			ardapg.Driver.NotNil(seed.CandidateUsers), seed.CandidateGroupID,
			seed.CandidateOrgUnitID, seed.SLADueAt, activationNo+1); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return r.GetWorkItem(ctx, id, "")
	}

	newStatus := existingStatus
	if seed.JobKey != nil && existingStatus != TaskStatusClaimed {
		newStatus = TaskStatusReady
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE workflow_tasks SET
			process_instance_key = COALESCE($3, process_instance_key),
			job_key = COALESCE($4, job_key),
			title = COALESCE(NULLIF($5, ''), title),
			description = COALESCE(NULLIF($6, ''), description),
			candidate_role = COALESCE(NULLIF($7, ''), candidate_role),
			candidate_users = CASE
				WHEN cardinality($8) > 0 THEN $8
				ELSE candidate_users
			END,
			candidate_group_id = COALESCE(NULLIF($9, ''), candidate_group_id),
			candidate_org_unit_id = COALESCE(NULLIF($10, ''), candidate_org_unit_id),
			status = $11,
			engine_state = CASE WHEN $4 IS NOT NULL THEN 'CREATED' ELSE engine_state END,
			engine_checked_at = CASE WHEN $4 IS NOT NULL THEN CURRENT_TIMESTAMP ELSE engine_checked_at END,
			sla_due_at = COALESCE($12, sla_due_at),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND case_id = $2
	`, existingID, seed.CaseID, seed.ProcessInstanceKey, seed.JobKey, seed.Title, seed.Description,
		seed.CandidateRole, ardapg.Driver.NotNil(seed.CandidateUsers), seed.CandidateGroupID,
		seed.CandidateOrgUnitID, newStatus, seed.SLADueAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetWorkItem(ctx, existingID, "")
}

func (r *CaseRepository) workItemSLADueAt(ctx context.Context, caseID string, stepCode string) (*time.Time, error) {
	tenantID, err := verifiedTenant(ctx)
	if err != nil {
		return nil, err
	}
	var dueAt sql.NullTime
	err = r.db.QueryRowContext(ctx, `
		SELECT COALESCE(
			(
				SELECT CASE stp.duration_unit
					WHEN 'MINUTE' THEN CURRENT_TIMESTAMP + (stp.duration_value * INTERVAL '1 minute')
					WHEN 'HOUR' THEN CURRENT_TIMESTAMP + (stp.duration_value * INTERVAL '1 hour')
				END
				FROM business_cases bc
				JOIN business_sla_task_policies stp ON stp.sla_policy_id = bc.sla_policy_id
				WHERE bc.tenant_id = $1 AND bc.id = $2
				  AND stp.step_code = $3
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
				WHERE bc.tenant_id = $1 AND bc.id = $2
				  AND sp.status = 'ACTIVE'
				  AND sp.effective_from <= CURRENT_TIMESTAMP
				  AND (sp.effective_to IS NULL OR sp.effective_to > CURRENT_TIMESTAMP)
				LIMIT 1
			)
		)
	`, tenantID, caseID, stepCode).Scan(&dueAt)
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
	tenantID, err := verifiedTenant(ctx)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, workItemSelectSQL()+` WHERE bc.tenant_id = $1 AND wt.id = $2`, tenantID, id)
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

// FindPendingWorkTask finds the current work item for a case + step, even when
// Zeebe has not yet assigned jobKey (status = ROUTING). Use for readiness checks
// where the caller needs to know whether the task row exists at all, not whether
// it is claimable yet.
func (r *CaseRepository) FindPendingWorkItemByStep(ctx context.Context, caseID, stepCode string) (*WorkItem, error) {
	if caseID == "" || stepCode == "" {
		return nil, nil
	}
	tenantID, err := verifiedTenant(ctx)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, workItemSelectSQL()+`
		WHERE bc.tenant_id = $1
		  AND wt.case_id = $2
		  AND wt.step_code = $3
		  AND wt.status NOT IN ('COMPLETED', 'CANCELLED')
		ORDER BY wt.updated_at DESC
		LIMIT 1
	`, tenantID, caseID, stepCode)
	item, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.decorate("", "")
	return &item, nil
}

func (r *CaseRepository) FindActiveWorkTask(ctx context.Context, caseID string, processInstanceKey int64) (*WorkItem, error) {
	if caseID == "" && processInstanceKey <= 0 {
		return nil, nil
	}
	where := []string{
		"bc.tenant_id = $1",
		"wt.status IN ('READY', 'CLAIMED')",
		"wt.job_key IS NOT NULL",
		"wt.job_key IS NOT NULL",
	}
	tenantID, err := verifiedTenant(ctx)
	if err != nil {
		return nil, err
	}
	args := []any{tenantID}
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

// FindWorkItemByJobKey returns the tenant-scoped work item bound to a Zeebe
// job or native user-task key. Nil means no local projection exists yet (for
// native tasks the caller still has to verify the task against the process).
func (r *CaseRepository) FindWorkItemByJobKey(ctx context.Context, jobKey int64) (*WorkItem, error) {
	if jobKey <= 0 {
		return nil, nil
	}
	tenantID, err := verifiedTenant(ctx)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, workItemSelectSQL()+`
		WHERE bc.tenant_id = $1 AND wt.job_key = $2
		ORDER BY wt.updated_at DESC
		LIMIT 1
	`, tenantID, jobKey)
	item, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
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
		"bc.status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')",
		// Only the case's live step is actionable inbox work. Rows left open by an
		// earlier activation (or an eager next-step placeholder) must not linger in
		// "Giao dịch đến" after the case has moved on.
		"(bc.current_step = '' OR wt.step_code = bc.current_step)",
	}
	where = append(where, incomingWorkItemWhere()...)
	return r.queryWorkItems(ctx, f, where, "INCOMING", false)
}

func incomingWorkItemWhere() []string {
	return []string{`(
		wt.status = 'ROUTING'
		OR (wt.status = 'READY' AND wt.job_key IS NOT NULL AND wt.assigned_to = '')
		OR (wt.status = 'CLAIMED' AND wt.job_key IS NOT NULL)
	)`}
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
		"wt.status IN ('ROUTING', 'READY', 'CLAIMED', 'COMPLETED')",
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

	tenantID, err := verifiedTenant(ctx)
	if err != nil {
		return nil, err
	}
	where := append([]string{"bc.tenant_id = $1"}, baseWhere...)
	args := []any{tenantID}
	add := func(sql string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(sql, len(args)))
	}
	if strings.TrimSpace(f.CreatedBy) != "" {
		add("bc.created_by = $%d", f.CreatedBy)
	}
	if strings.TrimSpace(f.AssignedTo) != "" {
		add("wt.assigned_to = $%d", f.AssignedTo)
	}
	if f.From != nil {
		add("bc.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		// Half-open [from, to): a date-only To (midnight business tz) must
		// cover through the end of that calendar day, so shift one day and
		// keep the exclusive bound.
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
	if f.Domain != "" && f.Domain != "ALL" {
		add("bc.case_type LIKE $%d", f.Domain+"%")
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
					wt.id, bc.id, bc.tenant_id, bc.case_code, bc.case_type, bc.primary_object_type, bc.primary_object_id,
					bc.process_instance_key, wt.job_key, wt.task_type, wt.step_code,
					wt.title, wt.description, wt.status, bc.status, bc.created_by,
					wt.candidate_role, wt.candidate_users, wt.candidate_group_id, wt.candidate_org_unit_id,
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
	tenantID, err := verifiedTenant(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var assignedTo, status string
	var jobKey sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT wt.assigned_to, wt.status, wt.job_key
		FROM workflow_tasks wt
		JOIN business_cases bc ON bc.id = wt.case_id
		WHERE bc.tenant_id = $1 AND wt.id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&assignedTo, &status, &jobKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status != TaskStatusReady || !jobKey.Valid {
		return nil, fmt.Errorf("task is not ready for claim")
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
		  AND EXISTS (SELECT 1 FROM business_cases bc WHERE bc.id = workflow_tasks.case_id AND bc.tenant_id = $4)
	`, id, TaskStatusClaimed, actor, tenantID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE business_cases
		SET assigned_to = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = (SELECT case_id FROM workflow_tasks WHERE id = $1)
		  AND tenant_id = $3
	`, id, actor, tenantID)
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
		SET status = $2, engine_state = 'COMPLETED', engine_checked_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE job_key = $1 AND status <> $2
	`, jobKey, TaskStatusCompleted)
	return err
}

// CancelOpenWorkItemsByProcessKey closes every open task of a case that just
// reached a terminal status.
func (r *CaseRepository) CancelOpenWorkItemsByProcessKey(ctx context.Context, processInstanceKey int64) error {
	if processInstanceKey == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE workflow_tasks
		SET status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE process_instance_key = $1 AND status IN ($3, $4, $5)
	`, processInstanceKey, TaskStatusCancelled, TaskStatusRouting, TaskStatusReady, TaskStatusClaimed)
	return err
}

// ReleaseExpiredClaims returns stale claims to the candidate pool and reports
// the engine job keys so the sweeper can unassign them on the engine side.
func (r *CaseRepository) ReleaseExpiredClaims(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		UPDATE workflow_tasks
		SET status = $1, assigned_to = '', assigned_at = NULL, claim_expires_at = NULL,
		    updated_at = CURRENT_TIMESTAMP
		WHERE status = $2 AND claim_expires_at IS NOT NULL AND claim_expires_at < CURRENT_TIMESTAMP
		RETURNING job_key
	`, TaskStatusReady, TaskStatusClaimed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0)
	for rows.Next() {
		var key sql.NullInt64
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if key.Valid {
			out = append(out, key.Int64)
		}
	}
	return out, rows.Err()
}

// ReconcileCandidate is one open, engine-bound task of an active case that the
// workflow reconciler re-checks against Zeebe's folded user task state.
type ReconcileCandidate struct {
	ID                 string
	CaseID             string
	ProcessInstanceKey int64
	JobKey             int64
	Status             string
}

// ListReconcileCandidates returns open READY/CLAIMED tasks of non-terminal
// cases that have not changed for at least graceSeconds, so the Elasticsearch
// exporter has caught up before the reconciler judges them.
func (r *CaseRepository) ListReconcileCandidates(ctx context.Context, graceSeconds, limit int) ([]ReconcileCandidate, error) {
	if graceSeconds < 0 {
		graceSeconds = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT wt.id, wt.case_id, bc.process_instance_key, wt.job_key, wt.status
		FROM workflow_tasks wt
		JOIN business_cases bc ON bc.id = wt.case_id
		WHERE wt.status IN ($3, $4)
		  AND wt.job_key IS NOT NULL
		  AND bc.process_instance_key IS NOT NULL
		  AND bc.status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
		  AND wt.updated_at < CURRENT_TIMESTAMP - ($1 * INTERVAL '1 second')
		ORDER BY wt.updated_at
		LIMIT $2
	`, graceSeconds, limit, TaskStatusReady, TaskStatusClaimed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ReconcileCandidate, 0)
	for rows.Next() {
		var c ReconcileCandidate
		var pik, jobKey sql.NullInt64
		if err := rows.Scan(&c.ID, &c.CaseID, &pik, &jobKey, &c.Status); err != nil {
			return nil, err
		}
		if !pik.Valid || !jobKey.Valid {
			continue
		}
		c.ProcessInstanceKey = pik.Int64
		c.JobKey = jobKey.Int64
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReconcileWorkItem closes a stale open task once its engine task reached a
// terminal state. It records COMPLETED when an APPLIED decision exists for the
// activation, otherwise CANCELLED, and returns the resulting status.
func (r *CaseRepository) ReconcileWorkItem(ctx context.Context, id string) (string, error) {
	var status string
	err := r.db.QueryRowContext(ctx, `
		UPDATE workflow_tasks wt
		SET status = CASE WHEN EXISTS (
				SELECT 1 FROM workflow_task_decisions d
				WHERE d.task_id = wt.id AND d.status = 'APPLIED'
			) THEN 'COMPLETED' ELSE 'CANCELLED' END,
		    engine_state = 'RECONCILED',
		    engine_checked_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE wt.id = $1
		RETURNING wt.status
	`, id).Scan(&status)
	return status, err
}

// TouchWorkItemChecked records that the engine task was still active for a row
// the reconciler inspected but did not close.
func (r *CaseRepository) TouchWorkItemChecked(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE workflow_tasks SET engine_checked_at = CURRENT_TIMESTAMP WHERE id = $1
	`, id)
	return err
}

func (r *CaseRepository) UserCanClaimRole(ctx context.Context, tenantID, userID string, groupIDs []string, roleCode string) (bool, error) {
	if strings.TrimSpace(tenantID) == "" || userID == "" || roleCode == "" {
		return false, nil
	}
	var ok bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM workflow_role_memberships
			WHERE tenant_id = $1
			  AND role_code = $2
			  AND principal_type = 'USER'
			  AND principal_id = $3
			  AND status = 'ACTIVE'
			  AND effective_from <= CURRENT_TIMESTAMP
			  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP)
		)
	`, tenantID, roleCode, userID).Scan(&ok)
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
				WHERE tenant_id = $1
				  AND role_code = $2
				  AND principal_type = 'GROUP'
				  AND principal_id = $3
				  AND status = 'ACTIVE'
				  AND effective_from <= CURRENT_TIMESTAMP
				  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP)
			)
		`, tenantID, roleCode, groupID).Scan(&ok)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func workItemSelectSQL() string {
	return `
		SELECT
			wt.id, bc.id, bc.tenant_id, bc.case_code, bc.case_type, bc.primary_object_type, bc.primary_object_id,
			bc.process_instance_key, wt.job_key, wt.task_type, wt.step_code,
			wt.title, wt.description, wt.status, bc.status, bc.created_by,
			wt.candidate_role, wt.candidate_users, wt.candidate_group_id, wt.candidate_org_unit_id,
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
		&item.ID, &item.CaseID, &item.TenantID, &item.CaseCode, &item.CaseType, &item.PrimaryObjectType, &item.PrimaryObjectID,
		&processInstanceKey, &jobKey, &item.TaskType, &item.StepCode,
		&item.Title, &item.Description, &item.Status, &item.TransactionStatus, &item.CreatedBy,
		&item.CandidateRole, ardapg.Driver.Scanner(&item.CandidateUsers), &item.CandidateGroupID, &item.CandidateOrgUnitID,
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
		item.CanView = true
		return
	}

	item.CanOpen = item.AssignedTo == "" || item.AssignedTo == userID
	item.CanClaim = item.Status == TaskStatusReady && item.AssignedTo == ""
	item.CanView = item.CanOpen || item.CanClaim
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
