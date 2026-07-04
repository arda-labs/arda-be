package worker

import (
	"context"
	"log/slog"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

type NotificationWorkers struct{}

func NewNotificationWorkers() *NotificationWorkers {
	return &NotificationWorkers{}
}

func (w *NotificationWorkers) SendEmailHandler(client worker.JobClient, job entities.Job) {
	w.complete(client, job, "notification.email")
}

func (w *NotificationWorkers) SendSMSHandler(client worker.JobClient, job entities.Job) {
	w.complete(client, job, "notification.sms")
}

func (w *NotificationWorkers) SendPushHandler(client worker.JobClient, job entities.Job) {
	w.complete(client, job, "notification.push")
}

func (w *NotificationWorkers) CustomerRegistrationResultHandler(client worker.JobClient, job entities.Job) {
	w.complete(client, job, "notification.customer_registration_result")
}

func (w *NotificationWorkers) complete(client worker.JobClient, job entities.Job, eventType string) {
	variables, err := job.GetVariablesAsMap()
	if err != nil {
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return
	}
	slog.Info("workflow notification job accepted",
		"jobKey", job.GetKey(),
		"eventType", eventType,
		"customerId", variables["customerId"],
		"customerStatus", variables["customerStatus"],
	)
	if _, err := client.NewCompleteJobCommand().JobKey(job.GetKey()).Send(context.Background()); err != nil {
		slog.Error("Failed to complete notification job", "jobKey", job.GetKey(), "err", err)
	}
}

func (w *NotificationWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
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
		slog.Error("Failed to fail notification job", "jobKey", job.GetKey(), "err", err)
	}
}
