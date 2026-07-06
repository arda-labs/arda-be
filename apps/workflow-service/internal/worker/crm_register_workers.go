package worker

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	crmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/crm"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

const (
	JobCRMRegisterValidate = "crm.customer.register.validate"
	JobCRMRegisterExecute  = "crm.customer.register.execute"
	JobCRMRegisterCancel   = "crm.customer.register.cancel"
	ErrorValidationFailed  = "VALIDATION_FAILED"
)

type CRMRegisterWorkers struct {
	crmClient  *crmclient.Client
	projection *CaseProjection
}

func NewCRMRegisterWorkers(crmClient *crmclient.Client, caseRepo *repository.CaseRepository) *CRMRegisterWorkers {
	return &CRMRegisterWorkers{
		crmClient:  crmClient,
		projection: NewCaseProjection(caseRepo),
	}
}

func (w *CRMRegisterWorkers) ValidateHandler(client worker.JobClient, job entities.Job) {
	logRegisterJob("validate", job)
	customerID, ok := w.customerID(client, job)
	if !ok {
		return
	}
	duplicateFound, err := w.crmClient.CheckDuplicateIdentity(context.Background(), customerID)
	if err != nil {
		w.failJob(client, job, "CRM Error: "+err.Error())
		return
	}
	if duplicateFound {
		if err := w.crmClient.UpdateCustomerStatus(context.Background(), customerID, "NEEDS_CHANGES"); err != nil {
			w.failJob(client, job, "CRM Error: "+err.Error())
			return
		}
		w.throwValidationError(client, job, "Trùng định danh khách hàng")
		return
	}
	if err := w.completeJob(client, job, nil); err != nil {
		return
	}
	w.projection.AfterServiceTaskCompleted(context.Background(), job.GetProcessInstanceKey(), "ST_Validate", "")
}

func (w *CRMRegisterWorkers) ExecuteHandler(client worker.JobClient, job entities.Job) {
	logRegisterJob("execute", job)
	customerID, ok := w.customerID(client, job)
	if !ok {
		return
	}
	if err := w.crmClient.UpdateCustomerStatus(context.Background(), customerID, "ACTIVE"); err != nil {
		w.failJob(client, job, "CRM Error: "+err.Error())
		return
	}
	if err := w.completeJob(client, job, map[string]any{
		"approvalStatus": "APPROVED",
		"customerStatus": "ACTIVE",
	}); err != nil {
		return
	}
	w.projection.AfterServiceTaskCompleted(context.Background(), job.GetProcessInstanceKey(), "ST_Execute", "")
	w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
}

func (w *CRMRegisterWorkers) CancelHandler(client worker.JobClient, job entities.Job) {
	logRegisterJob("cancel", job)
	customerID, ok := w.customerID(client, job)
	if !ok {
		return
	}
	if err := w.crmClient.UpdateCustomerStatus(context.Background(), customerID, "REJECTED"); err != nil {
		w.failJob(client, job, "CRM Error: "+err.Error())
		return
	}
	if err := w.completeJob(client, job, map[string]any{"customerStatus": "REJECTED"}); err != nil {
		return
	}
	w.projection.AfterServiceTaskCompleted(context.Background(), job.GetProcessInstanceKey(), "ST_Cancel", "")
	w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
}

func (w *CRMRegisterWorkers) throwValidationError(client worker.JobClient, job entities.Job, message string) {
	slog.Warn("workflow CRM register validation failed",
		"jobKey", job.GetKey(),
		"processInstanceKey", job.GetProcessInstanceKey(),
		"reason", message,
	)
	_, err := client.NewThrowErrorCommand().
		JobKey(job.GetKey()).
		ErrorCode(ErrorValidationFailed).
		ErrorMessage(message).
		Send(context.Background())
	if err != nil {
		slog.Error("failed to throw validation error", "jobKey", job.GetKey(), "err", err)
		w.failJob(client, job, "ThrowError failed: "+err.Error())
	}
}

func (w *CRMRegisterWorkers) customerID(client worker.JobClient, job entities.Job) (string, bool) {
	variables, err := job.GetVariablesAsMap()
	if err != nil {
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return "", false
	}
	customerID, _ := variables["customerId"].(string)
	if customerID == "" {
		customerID, _ = variables["primaryObjectId"].(string)
	}
	if customerID == "" {
		w.failJob(client, job, "Missing customerId")
		return "", false
	}
	return customerID, true
}

func (w *CRMRegisterWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) > 0 {
		withVars, err := cmd.VariablesFromMap(result)
		if err != nil {
			w.failJob(client, job, "Set variables error: "+err.Error())
			return err
		}
		_, err = withVars.Send(context.Background())
		if err != nil {
			slog.Error("failed to complete CRM register job", "jobKey", job.GetKey(), "err", err)
			return err
		}
		return nil
	}
	if _, err := cmd.Send(context.Background()); err != nil {
		slog.Error("failed to complete CRM register job", "jobKey", job.GetKey(), "err", err)
		return err
	}
	return nil
}

func (w *CRMRegisterWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow CRM register job failed",
		"jobKey", job.GetKey(),
		"jobType", job.GetType(),
		"retriesLeft", retries,
		"reason", reason,
	)
	_, err := client.NewFailJobCommand().
		JobKey(job.GetKey()).
		Retries(retries).
		ErrorMessage(reason).
		Send(context.Background())
	if err != nil {
		slog.Error("failed to fail CRM register job", "jobKey", job.GetKey(), "err", err)
	}
}

func logRegisterJob(handler string, job entities.Job) {
	variables, _ := job.GetVariablesAsMap()
	caseID, _ := variables["caseId"].(string)
	customerID, _ := variables["customerId"].(string)
	if customerID == "" {
		customerID, _ = variables["primaryObjectId"].(string)
	}
	slog.Info("workflow CRM register job received",
		"handler", handler,
		"jobKey", job.GetKey(),
		"jobType", job.GetType(),
		"elementId", job.GetElementId(),
		"processInstanceKey", job.GetProcessInstanceKey(),
		"caseId", caseID,
		"customerId", customerID,
	)
}

func (w *CRMRegisterWorkers) recordJobFailure(job entities.Job, reason string, retriesLeft int) {
	if w.projection == nil || w.projection.caseRepo == nil {
		return
	}
	variables, _ := job.GetVariablesAsMap()
	caseID, _ := variables["caseId"].(string)
	if caseID == "" {
		return
	}
	note := fmt.Sprintf(
		"jobKey=%d;jobType=%s;elementId=%s;retries=%d;error=%s",
		job.GetKey(),
		job.GetType(),
		job.GetElementId(),
		retriesLeft,
		url.QueryEscape(reason),
	)
	if err := w.projection.caseRepo.AddTimelineEvent(context.Background(), caseID, "JOB_FAILED", note); err != nil {
		slog.Error("failed to record job failure timeline", "caseId", caseID, "err", err)
	}
}
