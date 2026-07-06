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
	interval   time.Duration
}

func NewUserTaskProjector(rest *service.ZeebeRestClient, caseRepo *repository.CaseRepository) *UserTaskProjector {
	if rest == nil || !rest.Enabled() {
		return nil
	}
	return &UserTaskProjector{
		rest:       rest,
		caseRepo:   caseRepo,
		projection: NewCaseProjection(caseRepo),
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
			candidateRole := firstCandidateGroup(ut.CandidateGroups)
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
		return "Kiểm soát hồ sơ khách hàng"
	case "UT_MakerRevise":
		return "Maker bổ sung hồ sơ"
	default:
		return elementID
	}
}
