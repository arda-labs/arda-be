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

type CRMWorkers struct {
	crmClient *crmclient.Client
	caseRepo  *repository.CaseRepository
}

func NewCRMWorkers(crmClient *crmclient.Client, caseRepo *repository.CaseRepository) *CRMWorkers {
	return &CRMWorkers{crmClient: crmClient, caseRepo: caseRepo}
}

func (w *CRMWorkers) MarkSubmittedHandler(client worker.JobClient, job entities.Job) {
	logCRMJob("mark_submitted", job)
	w.updateStatus(client, job, "SUBMITTED", map[string]any{"customerStatus": "SUBMITTED"}, false, func() {
		w.markCaseAtStep(job, "Activity_CheckDuplicate", "")
	})
}

func (w *CRMWorkers) CheckDuplicateHandler(client worker.JobClient, job entities.Job) {
	logCRMJob("check_duplicate", job)
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
		if err := w.completeJob(client, job, map[string]any{"duplicateFound": duplicateFound}); err != nil {
			return
		}
		w.markCaseAtStep(job, "Activity_RequestChanges", "")
		return
	}
	if err := w.completeJob(client, job, map[string]any{"duplicateFound": duplicateFound}); err != nil {
		return
	}
	w.markCaseAtStep(job, "Activity_CheckerReview", "CUSTOMER_CHECKER")
}

func (w *CRMWorkers) RequestChangesHandler(client worker.JobClient, job entities.Job) {
	logCRMJob("request_changes", job)
	w.updateStatus(client, job, "NEEDS_CHANGES", map[string]any{"customerStatus": "NEEDS_CHANGES"}, false, func() {
		w.markCaseAtStep(job, "Activity_MakerRevise", "CUSTOMER_MAKER")
	})
}

func (w *CRMWorkers) RejectCustomerHandler(client worker.JobClient, job entities.Job) {
	logCRMJob("reject_customer", job)
	w.updateStatus(client, job, "REJECTED", map[string]any{"customerStatus": "REJECTED"}, true, nil)
}

func (w *CRMWorkers) CreateCustomerHandler(client worker.JobClient, job entities.Job) {
	logCRMJob("create_customer", job)
	w.updateStatus(client, job, "CREATED", map[string]any{"customerStatus": "CREATED"}, false, nil)
}

func (w *CRMWorkers) UpdateCustomerHandler(client worker.JobClient, job entities.Job) {
	logCRMJob("update_customer", job)
	w.updateStatus(client, job, "UPDATED", map[string]any{"customerStatus": "UPDATED"}, false, nil)
}

func (w *CRMWorkers) ApproveCustomerHandler(client worker.JobClient, job entities.Job) {
	logCRMJob("approve_customer", job)
	w.updateStatus(client, job, "ACTIVE", map[string]any{
		"approvalStatus": "APPROVED",
		"customerStatus": "ACTIVE",
	}, true, nil)
}

func (w *CRMWorkers) updateStatus(client worker.JobClient, job entities.Job, status string, result map[string]any, finishCase bool, afterComplete func()) {
	customerID, ok := w.customerID(client, job)
	if !ok {
		return
	}
	if err := w.crmClient.UpdateCustomerStatus(context.Background(), customerID, status); err != nil {
		w.failJob(client, job, "CRM Error: "+err.Error())
		return
	}
	slog.Info("workflow CRM job updated customer status", "customerId", customerID, "status", status)
	if finishCase {
		w.finishCase(context.Background(), job)
	}
	if err := w.completeJob(client, job, result); err != nil {
		return
	}
	if afterComplete != nil {
		afterComplete()
	}
}

func (w *CRMWorkers) finishCase(ctx context.Context, job entities.Job) {
	if w.caseRepo == nil {
		return
	}
	key := job.GetProcessInstanceKey()
	if key == 0 {
		return
	}
	if err := w.caseRepo.FinishCase(ctx, key, repository.CaseStatusCompleted); err != nil {
		slog.Error("failed to finish business case", "processInstanceKey", key, "err", err)
	}
}

func (w *CRMWorkers) markCaseAtStep(job entities.Job, stepID string, candidateRole string) {
	if w.caseRepo == nil {
		return
	}
	key := job.GetProcessInstanceKey()
	if key == 0 || stepID == "" {
		return
	}
	if err := w.caseRepo.MarkCaseAtStep(context.Background(), key, stepID, candidateRole); err != nil {
		slog.Error("failed to sync case step", "processInstanceKey", key, "stepId", stepID, "err", err)
	}
}

func (w *CRMWorkers) customerID(client worker.JobClient, job entities.Job) (string, bool) {
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

func (w *CRMWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) > 0 {
		withVars, err := cmd.VariablesFromMap(result)
		if err != nil {
			w.failJob(client, job, "Set variables error: "+err.Error())
			return err
		}
		_, err = withVars.Send(context.Background())
		if err != nil {
			slog.Error("Failed to complete CRM job", "jobKey", job.GetKey(), "err", err)
			return err
		}
		return nil
	}
	if _, err := cmd.Send(context.Background()); err != nil {
		slog.Error("Failed to complete CRM job", "jobKey", job.GetKey(), "err", err)
		return err
	}
	return nil
}

func (w *CRMWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	w.recordJobFailure(job, reason, int(retries))
	slog.Warn("workflow CRM job failed",
		"jobKey", job.GetKey(),
		"jobType", job.GetType(),
		"processInstanceKey", job.GetProcessInstanceKey(),
		"elementId", job.GetElementId(),
		"retriesLeft", retries,
		"reason", reason,
	)
	_, err := client.NewFailJobCommand().
		JobKey(job.GetKey()).
		Retries(retries).
		ErrorMessage(reason).
		Send(context.Background())
	if err != nil {
		slog.Error("Failed to fail CRM job", "jobKey", job.GetKey(), "err", err)
	}
}

func (w *CRMWorkers) recordJobFailure(job entities.Job, reason string, retriesLeft int) {
	if w.caseRepo == nil {
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
	if err := w.caseRepo.AddTimelineEvent(context.Background(), caseID, "JOB_FAILED", note); err != nil {
		slog.Error("failed to record job failure timeline", "caseId", caseID, "err", err)
	}
}

func logCRMJob(handler string, job entities.Job) {
	variables, _ := job.GetVariablesAsMap()
	customerID, _ := variables["customerId"].(string)
	if customerID == "" {
		customerID, _ = variables["primaryObjectId"].(string)
	}
	caseID, _ := variables["caseId"].(string)
	slog.Info("workflow CRM job received",
		"handler", handler,
		"jobKey", job.GetKey(),
		"jobType", job.GetType(),
		"elementId", job.GetElementId(),
		"processInstanceKey", job.GetProcessInstanceKey(),
		"caseId", caseID,
		"customerId", customerID,
	)
}
