package worker

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// UserTaskProjector is the single writer for native user task work items on v2 processes.
// It reads Zeebe USER_TASK exporter records from Elasticsearch (Camunda 8.5 without Tasklist).
type UserTaskProjector struct {
	rest       *service.ZeebeRestClient
	caseRepo   *repository.CaseRepository
	projection *CaseProjection
	assign     *service.AssignmentResolver
	interval   time.Duration
}

func NewUserTaskProjector(rest *service.ZeebeRestClient, caseRepo *repository.CaseRepository, assign *service.AssignmentResolver) *UserTaskProjector {
	if rest == nil || !rest.Enabled() {
		return nil
	}
	return &UserTaskProjector{
		rest:       rest,
		caseRepo:   caseRepo,
		projection: NewCaseProjection(caseRepo),
		assign:     assign,
		interval:   2 * time.Second,
	}
}

func (p *UserTaskProjector) Run(ctx context.Context) {
	if p == nil || p.rest == nil || !p.rest.Enabled() {
		return
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	slog.Info("user task projector started", "interval", p.interval.String())
	for {
		select {
		case <-ctx.Done():
			slog.Info("user task projector stopped")
			return
		case <-ticker.C:
			p.projectOnce(ctx)
		}
	}
}

func (p *UserTaskProjector) projectOnce(ctx context.Context) {
	cases, err := p.caseRepo.ListActiveCasesWithProcess(ctx, 100)
	if err != nil {
		slog.Warn("user task projector: list active cases failed", "err", err)
		return
	}
	for _, bc := range cases {
		if bc.ProcessInstanceKey == nil || *bc.ProcessInstanceKey == 0 {
			continue
		}
		if bc.BpmnProcessID == nil || !strings.Contains(*bc.BpmnProcessID, "-v2") {
			continue
		}
		tasks, err := p.rest.SearchUserTasks(ctx, *bc.ProcessInstanceKey, "CREATED")
		if err != nil {
			slog.Debug("user task projector: search skipped",
				"caseId", bc.ID,
				"processInstanceKey", *bc.ProcessInstanceKey,
				"err", err,
			)
			continue
		}
		for _, ut := range tasks {
			if !service.IsNativeUserTaskElement(ut.ElementID) {
				continue
			}
			// Assignment rules (case_type + step_code → role/memberships)
			// win over BPMN-declared candidate groups when configured.
			candidateRole := firstCandidateGroup(ut.CandidateGroups)
			var candidateUsers []string
			if p.assign.Enabled() {
				result := p.assign.Resolve(ctx, service.AssignmentRequest{
					CaseType:  bc.CaseType,
					StepCode:  ut.ElementID,
					TenantID:  bc.TenantID,
					CreatedBy: bc.CreatedBy,
				})
				if result.Resolved {
					candidateRole = result.RoleCode
					candidateUsers = result.CandidateUsers
					// DIRECT mode pins the task to a single user right away;
					// CANDIDATE_POOL mode keeps it claimable by the pool.
					if strings.EqualFold(result.AssignmentMode, "DIRECT") && len(candidateUsers) > 0 && ut.Assignee == "" {
						if err := p.rest.AssignUserTask(ctx, ut.UserTaskKey, candidateUsers[0]); err != nil {
							slog.Debug("user task projector: direct assignment skipped",
								"userTaskKey", ut.UserTaskKey, "assignee", candidateUsers[0], "err", err)
						}
					}
				}
			}
			title := userTaskTitle(ut.ElementID)
			key := ut.UserTaskKey
			pik := ut.ProcessInstanceKey
			p.projection.UpsertUserTaskWorkItem(ctx, repository.WorkItemSeed{
				CaseID:             bc.ID,
				ProcessInstanceKey: &pik,
				JobKey:             &key,
				TaskType:           "zeebe.userTask",
				StepCode:           ut.ElementID,
				CandidateRole:      candidateRole,
				CandidateUsers:     candidateUsers,
				Title:              title,
				Description:        bc.Title,
			})
		}
	}
}

func firstCandidateGroup(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return strings.TrimSpace(groups[0])
}

func userTaskTitle(elementID string) string {
	switch elementID {
	case "UT_CheckerReview":
		return "Phê duyệt hồ sơ khách hàng"
	case "UT_MakerRevise":
		return "Chỉnh sửa hồ sơ"
	default:
		return elementID
	}
}
