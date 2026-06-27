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

func (w *CRMWorkers) CreateCustomerHandler(client worker.JobClient, job entities.Job) {
	key := job.GetKey()
	slog.Info("Worker crm.create_customer activated", "jobKey", key)

	variables, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Error("Failed to parse variables", "err", err)
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return
	}

	customerId, _ := variables["customerId"].(string)
	if customerId == "" {
		slog.Error("Missing customerId in job variables")
		w.failJob(client, job, "Missing customerId")
		return
	}

	// Update DB status to CREATED
	ctx := context.Background()
	err = w.customerRepo.UpdateStatus(ctx, customerId, "CREATED")
	if err != nil {
		slog.Error("Failed to update customer status to CREATED", "customerId", customerId, "err", err)
		w.failJob(client, job, "DB Error: "+err.Error())
		return
	}

	slog.Info("Customer created in CRM database", "customerId", customerId)

	// Complete the job, passing updated variables if needed
	_, err = client.NewCompleteJobCommand().JobKey(key).Send(ctx)
	if err != nil {
		slog.Error("Failed to complete job", "jobKey", key, "err", err)
	}
}

func (w *CRMWorkers) UpdateCustomerHandler(client worker.JobClient, job entities.Job) {
	key := job.GetKey()
	slog.Info("Worker crm.update_customer activated", "jobKey", key)

	variables, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Error("Failed to parse variables", "err", err)
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return
	}

	customerId, _ := variables["customerId"].(string)
	if customerId == "" {
		slog.Error("Missing customerId in job variables")
		w.failJob(client, job, "Missing customerId")
		return
	}

	// Update DB status to UPDATED
	ctx := context.Background()
	err = w.customerRepo.UpdateStatus(ctx, customerId, "UPDATED")
	if err != nil {
		slog.Error("Failed to update customer status to UPDATED", "customerId", customerId, "err", err)
		w.failJob(client, job, "DB Error: "+err.Error())
		return
	}

	slog.Info("Customer updated in CRM database", "customerId", customerId)

	_, err = client.NewCompleteJobCommand().JobKey(key).Send(ctx)
	if err != nil {
		slog.Error("Failed to complete job", "jobKey", key, "err", err)
	}
}

func (w *CRMWorkers) ApproveCustomerHandler(client worker.JobClient, job entities.Job) {
	key := job.GetKey()
	slog.Info("Worker crm.approve_customer activated", "jobKey", key)

	variables, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Error("Failed to parse variables", "err", err)
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return
	}

	customerId, _ := variables["customerId"].(string)
	if customerId == "" {
		slog.Error("Missing customerId in job variables")
		w.failJob(client, job, "Missing customerId")
		return
	}

	// Update DB status to APPROVED
	ctx := context.Background()
	err = w.customerRepo.UpdateStatus(ctx, customerId, "APPROVED")
	if err != nil {
		slog.Error("Failed to update customer status to APPROVED", "customerId", customerId, "err", err)
		w.failJob(client, job, "DB Error: "+err.Error())
		return
	}

	slog.Info("Customer approved in CRM database", "customerId", customerId)

	// Build response payload showing approval confirmation
	resultMap := map[string]any{
		"approvalStatus": "APPROVED",
		"approvedAt":     job.GetElementId(), // just metadata helper
	}

	cmd, err := client.NewCompleteJobCommand().JobKey(key).VariablesFromMap(resultMap)
	if err != nil {
		slog.Error("Failed to set variables on complete job command", "err", err)
		w.failJob(client, job, "Set variables error: "+err.Error())
		return
	}

	_, err = cmd.Send(ctx)
	if err != nil {
		slog.Error("Failed to complete job", "jobKey", key, "err", err)
	}
}

func (w *CRMWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	ctx := context.Background()
	_, err := client.NewFailJobCommand().
		JobKey(job.GetKey()).
		Retries(job.GetRetries() - 1).
		ErrorMessage(reason).
		Send(ctx)
	if err != nil {
		slog.Error("Failed to fail job", "jobKey", job.GetKey(), "err", err)
	}
}
