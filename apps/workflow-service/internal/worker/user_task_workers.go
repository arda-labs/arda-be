package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

var UserTaskJobTypes = []string{
	"workflow.customer_checker_review",
	"workflow.customer_maker_revise",
	"workflow.customer_risk_review",
	"workflow.hrm_registration_review",
	"workflow.hrm_registration_approve",
	"workflow.finance_incoming_classify",
	"workflow.finance_incoming_approve",
	"workflow.finance_outgoing_verify",
	"workflow.finance_outgoing_approve",
}

type UserTaskWorkers struct {
	caseRepo *repository.CaseRepository
	broker   *UserTaskBroker
}

func NewUserTaskWorkers(caseRepo *repository.CaseRepository, broker *UserTaskBroker) *UserTaskWorkers {
	return &UserTaskWorkers{caseRepo: caseRepo, broker: broker}
}

func (w *UserTaskWorkers) Handler(client worker.JobClient, job entities.Job) {
	jobKey := job.GetKey()
	variables, _ := job.GetVariablesAsMap()
	headers, _ := job.GetCustomHeadersAsMap()
	caseID, _ := variables["caseId"].(string)
	candidateRole := headers["candidateRole"]

	slog.Info("workflow user task received",
		"jobKey", jobKey,
		"jobType", job.GetType(),
		"elementId", job.GetElementId(),
		"processInstanceKey", job.GetProcessInstanceKey(),
		"caseId", caseID,
		"candidateRole", candidateRole,
	)

	w.persistWorkItem(job, caseID, candidateRole)

	parked := ParkedUserTask{
		JobKey:             jobKey,
		JobType:            job.GetType(),
		ElementID:          job.GetElementId(),
		ProcessInstanceKey: job.GetProcessInstanceKey(),
		CaseID:             caseID,
		CandidateRole:      candidateRole,
	}
	wait := w.broker.Register(parked)
	defer w.broker.Remove(jobKey)

	deadline := time.Until(time.UnixMilli(job.GetDeadline()))
	if deadline <= 0 {
		deadline = 30 * time.Minute
	}
	if deadline > time.Minute {
		deadline -= 30 * time.Second
	}

	select {
	case result := <-wait:
		cmd := client.NewCompleteJobCommand().JobKey(jobKey)
		if len(result) > 0 {
			withVars, err := cmd.VariablesFromMap(result)
			if err != nil {
				w.failJob(client, job, "Set variables error: "+err.Error())
				return
			}
			if _, err := withVars.Send(context.Background()); err != nil {
				slog.Error("workflow user task complete failed", "jobKey", jobKey, "err", err)
			}
			return
		}
		if _, err := cmd.Send(context.Background()); err != nil {
			slog.Error("workflow user task complete failed", "jobKey", jobKey, "err", err)
		}
	case <-time.After(deadline):
		w.failJob(client, job, "user task timed out waiting for completion")
	}
}

func (w *UserTaskWorkers) persistWorkItem(job entities.Job, caseID, candidateRole string) {
	if w.caseRepo == nil || caseID == "" {
		return
	}
	title := taskLabel(job.GetType())
	customerName, _ := job.GetVariablesAsMap()
	name, _ := customerName["customerName"].(string)
	_, err := w.caseRepo.UpsertWorkItem(context.Background(), repository.WorkItemSeed{
		CaseID:             caseID,
		ProcessInstanceKey: int64Ptr(job.GetProcessInstanceKey()),
		JobKey:             int64Ptr(job.GetKey()),
		TaskType:           job.GetType(),
		StepCode:           job.GetElementId(),
		CandidateRole:      candidateRole,
		Title:              title,
		Description:        name,
	})
	if err != nil {
		slog.Error("failed to persist user task work item",
			"caseId", caseID,
			"jobKey", job.GetKey(),
			"err", err,
		)
	}
	if err := w.caseRepo.MarkCaseAtStep(context.Background(), job.GetProcessInstanceKey(), job.GetElementId(), candidateRole); err != nil {
		slog.Error("failed to sync user task case step",
			"processInstanceKey", job.GetProcessInstanceKey(),
			"stepId", job.GetElementId(),
			"err", err,
		)
	}
}

func (w *UserTaskWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow user task failed",
		"jobKey", job.GetKey(),
		"jobType", job.GetType(),
		"processInstanceKey", job.GetProcessInstanceKey(),
		"retriesLeft", retries,
		"reason", reason,
	)
	_, err := client.NewFailJobCommand().
		JobKey(job.GetKey()).
		Retries(retries).
		ErrorMessage(reason).
		Send(context.Background())
	if err != nil {
		slog.Error("failed to fail user task job", "jobKey", job.GetKey(), "err", err)
	}
}

func int64Ptr(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func taskLabel(jobType string) string {
	switch jobType {
	case "workflow.customer_checker_review":
		return "Kiểm soát hồ sơ khách hàng"
	case "workflow.customer_maker_revise":
		return "Maker bổ sung hồ sơ"
	case "workflow.customer_risk_review":
		return "Rà soát rủi ro khách hàng"
	case "workflow.hrm_registration_review":
		return "Rà soát đăng ký nhân sự"
	case "workflow.hrm_registration_approve":
		return "Phê duyệt đăng ký nhân sự"
	default:
		return jobType
	}
}
