package service

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

// EagerUserTask describes the next inbox row to materialize before Zeebe
// projection catches up.
type EagerUserTask struct {
	StepCode      string
	CandidateRole string
	Title         string
}

// InitialUserTaskForCaseType is the first native user task after process start.
func InitialUserTaskForCaseType(caseType string) (EagerUserTask, bool) {
	switch caseType {
	case "CUSTOMER_REGISTRATION", "CUSTOMER_ADJUSTMENT":
		return EagerUserTask{
			StepCode:      "UT_MakerRevise",
			CandidateRole: "CUSTOMER_MAKER",
			Title:         "Chỉnh sửa hồ sơ",
		}, true
	default:
		return EagerUserTask{}, false
	}
}

// SeedEagerUserTask upserts a READY work item so workbench lists update
// immediately; UserTaskProjector later binds the real Zeebe userTaskKey.
func SeedEagerUserTask(ctx context.Context, caseRepo *repository.CaseRepository, bc *repository.BusinessCase, task EagerUserTask) {
	if caseRepo == nil || bc == nil || task.StepCode == "" {
		return
	}
	seed := repository.WorkItemSeed{
		CaseID:        bc.ID,
		TaskType:      "zeebe.userTask",
		StepCode:      task.StepCode,
		CandidateRole: task.CandidateRole,
		Title:         task.Title,
		Description:   bc.Title,
	}
	if bc.ProcessInstanceKey != nil {
		seed.ProcessInstanceKey = bc.ProcessInstanceKey
	}
	if _, err := caseRepo.UpsertWorkItem(ctx, seed); err != nil {
		slog.Warn("eager work item seed failed",
			"caseId", bc.ID,
			"caseType", bc.CaseType,
			"stepCode", task.StepCode,
			"err", err,
		)
		return
	}
	if bc.ProcessInstanceKey != nil {
		if err := caseRepo.MarkCaseAtStep(ctx, *bc.ProcessInstanceKey, task.StepCode, task.CandidateRole); err != nil {
			slog.Warn("eager mark case at step failed",
				"caseId", bc.ID,
				"stepCode", task.StepCode,
				"err", err,
			)
		}
	}
	slog.Info("eager work item seeded",
		"caseId", bc.ID,
		"caseType", bc.CaseType,
		"stepCode", task.StepCode,
		"candidateRole", task.CandidateRole,
	)
}
