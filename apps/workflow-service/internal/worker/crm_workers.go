package worker

import (
	"context"
	"log/slog"

	crmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/crm"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

type CRMWorkers struct {
	crmClient *crmclient.Client
}

func NewCRMWorkers(crmClient *crmclient.Client) *CRMWorkers {
	return &CRMWorkers{crmClient: crmClient}
}

func (w *CRMWorkers) MarkSubmittedHandler(client worker.JobClient, job entities.Job) {
	w.updateStatus(client, job, "SUBMITTED", map[string]any{"customerStatus": "SUBMITTED"})
}

func (w *CRMWorkers) CheckDuplicateHandler(client worker.JobClient, job entities.Job) {
	customerID, ok := w.customerID(client, job)
	if !ok {
		return
	}
	duplicateFound, err := w.crmClient.CheckDuplicateIdentity(context.Background(), customerID)
	if err != nil {
		w.failJob(client, job, "CRM Error: "+err.Error())
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
	if err := w.crmClient.UpdateCustomerStatus(context.Background(), customerID, status); err != nil {
		w.failJob(client, job, "CRM Error: "+err.Error())
		return
	}
	slog.Info("workflow CRM job updated customer status", "customerId", customerID, "status", status)
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
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) > 0 {
		withVars, err := cmd.VariablesFromMap(result)
		if err != nil {
			w.failJob(client, job, "Set variables error: "+err.Error())
			return
		}
		_, err = withVars.Send(context.Background())
		if err != nil {
			slog.Error("Failed to complete CRM job", "jobKey", job.GetKey(), "err", err)
		}
		return
	}
	if _, err := cmd.Send(context.Background()); err != nil {
		slog.Error("Failed to complete CRM job", "jobKey", job.GetKey(), "err", err)
	}
}

func (w *CRMWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	_, err := client.NewFailJobCommand().
		JobKey(job.GetKey()).
		Retries(retries).
		ErrorMessage(reason).
		Send(context.Background())
	if err != nil {
		slog.Error("Failed to fail CRM job", "jobKey", job.GetKey(), "err", err)
	}
}
