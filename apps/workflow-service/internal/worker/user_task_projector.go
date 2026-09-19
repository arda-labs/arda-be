package worker

import (
	"context"
	"log/slog"
	"strings"
	"time"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
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
		// Registry v2 discovery: the case's pinned step registry decides what
		// is a human task, not a name substring. Case types without registry
		// rows are not projected (fail-closed, logged below).
		version := 1
		if pinned, err := p.caseRepo.CaseRegistryVersion(ctx, bc.ID); err == nil && pinned > 0 {
			version = pinned
		}
		steps, err := p.caseRepo.ListCaseTypeSteps(ctx, bc.CaseType, version)
		if err != nil {
			slog.Warn("user task projector: registry lookup failed",
				"caseId", bc.ID, "caseType", bc.CaseType, "registryVersion", version, "err", err)
			continue
		}
		if len(steps) == 0 {
			slog.Debug("user task projector: case type has no registry steps",
				"caseId", bc.ID, "caseType", bc.CaseType, "registryVersion", version)
			continue
		}
		stepByElement := make(map[string]repository.CaseTypeStep, len(steps))
		for _, step := range steps {
			stepByElement[step.ElementID] = step
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
			if _, known := stepByElement[ut.ElementID]; !known {
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
			// The projector runs on a system context without request
			// metadata; scope repo writes to the case's own tenant.
			tenantCtx := ardametadata.AppendToOutgoing(ctx, ardametadata.Context{TenantID: bc.TenantID})
			p.projection.UpsertUserTaskWorkItem(tenantCtx, repository.WorkItemSeed{
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
		return "Phê duyệt"
	case "UT_MakerRevise":
		return "Chỉnh sửa hồ sơ"
	case "UT_MakerInput":
		return "Nhập liệu"
	case "UT_TWRevalidate":
		return "Tái thẩm định"
	case "UT_PGDReview":
		return "Xem xét cấp Phó giám đốc"
	case "UT_GDReview":
		return "Phê duyệt cấp Giám đốc"
	case "UT_BoardReview":
		return "Phê duyệt Hội đồng tín dụng"
	default:
		return elementID
	}
}
