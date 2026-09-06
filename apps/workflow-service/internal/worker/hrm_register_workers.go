package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	hrmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/hrm"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// JobHRMRegister topics for the hrm-employee-registration-v2 process.
const (
	JobHRMRegisterValidate = "hrm.employee.register.validate"
	JobHRMRegisterExecute  = "hrm.employee.register.execute"
	JobHRMRegisterCancel   = "hrm.employee.register.cancel"
)

// HRMRegisterWorkers handle the hrm-employee-registration-v2 jobs: validate
// and execute/cancel call back into hrm-service (EmployeeCommandService).
type HRMRegisterWorkers struct {
	hrmClient  *hrmclient.Client
	projection *CaseProjection
}

func NewHRMRegisterWorkers(hrmClient *hrmclient.Client, caseRepo *repository.CaseRepository) *HRMRegisterWorkers {
	return &HRMRegisterWorkers{hrmClient: hrmClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *HRMRegisterWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *HRMRegisterWorkers) registrationID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["employeeRegistrationId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing employeeRegistrationId variable")
	}
	return id, nil
}

func (w *HRMRegisterWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logRegisterJob("hrm-validate", job)
		ctx := context.Background()
		id, err := w.registrationID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		ok, message, err := w.hrmClient.CheckRegistration(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "HRM Error: "+err.Error())
			return
		}
		if !ok {
			throwValidationError(client, job, message)
			return
		}
		_ = w.complete(ctx, client, job, nil)
	}
}

func (w *HRMRegisterWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logRegisterJob("hrm-execute", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		id, _ := vars["employeeRegistrationId"].(string)
		if id == "" {
			id, _ = vars["primaryObjectId"].(string)
		}
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		employeeID, err := w.hrmClient.SettleRegistration(crmJobContext(job), id, actor)
		if err != nil {
			w.failJob(client, job, "HRM Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "APPROVED",
			"employeeId":     employeeID,
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
		slog.Info("hrm registration settled", "id", id, "employee", employeeID)
	}
}

func (w *HRMRegisterWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logRegisterJob("hrm-cancel", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		id, _ := vars["employeeRegistrationId"].(string)
		if id == "" {
			id, _ = vars["primaryObjectId"].(string)
		}
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if note == "" {
			note = "Rejected by reviewer"
		}
		if err := w.hrmClient.RejectRegistration(crmJobContext(job), id, decidedBy, note); err != nil {
			w.failJob(client, job, "HRM Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *HRMRegisterWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) > 0 {
		withVars, err := cmd.VariablesFromMap(result)
		if err != nil {
			return err
		}
		_, err = withVars.Send(ctx)
		return err
	}
	_, err := cmd.Send(ctx)
	return err
}

func (w *HRMRegisterWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow hrm job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("hrm fail-job send", "err", err)
	}
}
