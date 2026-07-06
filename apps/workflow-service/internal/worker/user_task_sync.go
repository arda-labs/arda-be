package worker

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// UserTaskSync polls Zeebe REST for native user tasks on active cases and projects work items.
type UserTaskSync struct {
	rest       *service.ZeebeRestClient
	caseRepo   *repository.CaseRepository
	projection *CaseProjection
	interval   time.Duration
}

func NewUserTaskSync(rest *service.ZeebeRestClient, caseRepo *repository.CaseRepository) *UserTaskSync {
	if rest == nil || !rest.Enabled() {
		return nil
	}
	return &UserTaskSync{
		rest:       rest,
		caseRepo:   caseRepo,
		projection: NewCaseProjection(caseRepo),
		interval:   5 * time.Second,
	}
}

func (s *UserTaskSync) Run(ctx context.Context) {
	if s == nil || s.rest == nil || !s.rest.Enabled() {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	slog.Info("user task sync started", "interval", s.interval.String())
	for {
		select {
		case <-ctx.Done():
			slog.Info("user task sync stopped")
			return
		case <-ticker.C:
			s.syncOnce(ctx)
		}
	}
}

func (s *UserTaskSync) syncOnce(ctx context.Context) {
	cases, err := s.caseRepo.ListActiveCasesWithProcess(ctx, 100)
	if err != nil {
		slog.Warn("user task sync: list active cases failed", "err", err)
		return
	}
	for _, bc := range cases {
		if bc.ProcessInstanceKey == nil || *bc.ProcessInstanceKey == 0 {
			continue
		}
		if bc.BpmnProcessID == nil || !strings.Contains(*bc.BpmnProcessID, "-v2") {
			continue
		}
		tasks, err := s.rest.SearchUserTasks(ctx, *bc.ProcessInstanceKey, "CREATED")
		if err != nil {
			slog.Warn("user task sync: search failed",
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
			s.projection.UpsertUserTaskWorkItem(ctx, repository.WorkItemSeed{
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
