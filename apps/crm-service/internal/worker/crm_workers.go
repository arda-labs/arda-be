package worker

import (
	"context"
	"log/slog"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
)

type CRMWorkers struct {
	customerRepo *repository.CustomerRepository
}

func NewCRMWorkers(customerRepo *repository.CustomerRepository) *CRMWorkers {
	return &CRMWorkers{customerRepo: customerRepo}
}

func (w *CRMWorkers) MarkSubmittedHandler(client worker.JobClient, job entities.Job) {
	w.updateStatus(client, job, "SUBMITTED", map[string]any{"customerStatus": "SUBMITTED"})
}

func (w *CRMWorkers) CheckDuplicateHandler(client worker.JobClient, job entities.Job) {
	customerID, ok := w.customerID(client, job)
	if !ok {
		return
	}
	duplicateFound, err := w.customerRepo.HasDuplicateIdentity(context.Background(), customerID)
	if err != nil {
		w.failJob(client, job, "DB Error: "+err.Error())
		return
	}
	w.completeJob(client, job, map[string]any{"duplicateFound": duplicateFound})
}

func (w *CRMWorkers) RequestChangesHandler(client worker.JobClient, job entities.Job) {
	w.updateStatus(client, job, "NEEDS_CHANGES", map[string]any{"customerStatus": "NEEDS_CHANGES"})
}

func (w *CRMWorkers) RejectCustomerHandler(client worker.JobClient, job entities.Job) {
	w.updateStatus(client, job, "REJECTED", map[string]any{"customerStatus": "REJECTED"})
}

func (w *CRMWorkers) CreateCustomerHandler(client worker.JobClient, job entities.Job) {
	w.updateStatus(client, job, "CREATED", map[string]any{"customerStatus": "CREATED"})
}

func (w *CRMWorkers) UpdateCustomerHandler(client worker.JobClient, job entities.Job) {
	w.updateStatus(client, job, "UPDATED", map[string]any{"customerStatus": "UPDATED"})
}

func (w *CRMWorkers) ApproveCustomerHandler(client worker.JobClient, job entities.Job) {
	w.updateStatus(client, job, "APPROVED", map[string]any{
		"approvalStatus": "APPROVED",
		"customerStatus": "APPROVED",
	})
}

func (w *CRMWorkers) updateStatus(client worker.JobClient, job entities.Job, status string, result map[string]any) {
	customerID, ok := w.customerID(client, job)
	if !ok {
		return
	}
	if err := w.customerRepo.UpdateStatus(context.Background(), customerID, status); err != nil {
		slog.Error("Failed to update customer status", "customerId", customerID, "status", status, "err", err)
		w.failJob(client, job, "DB Error: "+err.Error())
		return
	}
	slog.Info("Customer status updated", "customerId", customerID, "status", status)
	w.completeJob(client, job, result)
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

func (w *CRMWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) {
	ctx := context.Background()
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) > 0 {
		withVars, err := cmd.VariablesFromMap(result)
		if err != nil {
			w.failJob(client, job, "Set variables error: "+err.Error())
			return
		}
		_, err = withVars.Send(ctx)
		if err != nil {
			slog.Error("Failed to complete job", "jobKey", job.GetKey(), "err", err)
		}
		return
	}
	if _, err := cmd.Send(ctx); err != nil {
		slog.Error("Failed to complete job", "jobKey", job.GetKey(), "err", err)
	}
}

func (w *CRMWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	ctx := context.Background()
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	_, err := client.NewFailJobCommand().
		JobKey(job.GetKey()).
		Retries(retries).
		ErrorMessage(reason).
		Send(ctx)
	if err != nil {
		slog.Error("Failed to fail job", "jobKey", job.GetKey(), "err", err)
	}
}
