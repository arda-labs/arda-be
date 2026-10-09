package repository

import (
	"context"
	"fmt"
	"time"
)

// EmitSLANotifications adds each crossed milestone once. Thresholds are
// percentages of the task's created-to-due interval (normally 50, 90, 100).
func (r *CaseRepository) EmitSLANotifications(ctx context.Context, thresholds []int) error {
	if len(thresholds) == 0 {
		thresholds = []int{50, 90, 100}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
		SELECT wt.id, bc.tenant_id, wt.task_type, wt.title, wt.created_at, wt.sla_due_at,
		       wt.candidate_users, wt.candidate_group_id, wt.candidate_role,
		       wt.candidate_org_unit_id, wt.assigned_to
		FROM workflow_tasks wt JOIN business_cases bc ON bc.id = wt.case_id
		WHERE wt.status IN ($1, $2, $3) AND wt.sla_due_at IS NOT NULL
		  AND wt.sla_due_at > wt.created_at AND bc.status NOT IN ('COMPLETED','REJECTED','CANCELLED')
		ORDER BY wt.sla_due_at LIMIT 500 FOR UPDATE OF wt SKIP LOCKED
	`, TaskStatusRouting, TaskStatusReady, TaskStatusClaimed)
	if err != nil {
		return err
	}
	type task struct {
		id, tenant, taskType, title    string
		created, due                   time.Time
		users                          []string
		group, role, orgUnit, assigned string
	}
	var tasks []task
	for rows.Next() {
		var item task
		if err := rows.Scan(&item.id, &item.tenant, &item.taskType, &item.title, &item.created, &item.due, &item.users, &item.group, &item.role, &item.orgUnit, &item.assigned); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	now := time.Now()
	for _, item := range tasks {
		window := item.due.Sub(item.created)
		for _, milestone := range thresholds {
			if milestone <= 0 || milestone > 100 {
				return fmt.Errorf("invalid workflow SLA milestone %d", milestone)
			}
			if now.Before(item.created.Add(window * time.Duration(milestone) / 100)) {
				continue
			}
			kind, subject, code := "sla_warning", "arda.workflow.task.sla_warning.v1", "workflow.task.sla_warning"
			if milestone == 100 {
				kind, subject, code = "overdue", "arda.workflow.task.overdue.v1", "workflow.task.overdue"
			}
			dedupe := fmt.Sprintf("task:%s:sla:%d", item.id, milestone)
			users := item.users
			if item.assigned != "" {
				users = []string{item.assigned}
			}
			payload := taskEventPayload(dedupe, item.tenant, item.id, item.taskType, item.title, "/workflow/tasks/"+item.id, users,
				optionalString(item.group), optionalString(item.role), optionalString(item.orgUnit))
			payload["sla_milestone"] = milestone
			payload["sla_event"] = kind
			if err := enqueueWorkflowEvent(ctx, tx, subject, code, dedupe, payload); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
