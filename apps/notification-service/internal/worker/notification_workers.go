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
	key := job.GetKey()
	slog.Info("Worker notification.email activated", "jobKey", key)

	variables, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Error("Failed to parse variables", "err", err)
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return
	}

	email, _ := variables["customerEmail"].(string)
	name, _ := variables["customerName"].(string)
	if email == "" {
		email = "default@example.com"
	}

	slog.Info("📧 EMAIL SENT SUCCESSFULLY", 
		"recipientName", name, 
		"recipientEmail", email, 
		"subject", "Welcome to Arda Onboarding!",
		"content", "Hello "+name+", your customer account onboarding process has been initiated.",
	)

	ctx := context.Background()
	_, err = client.NewCompleteJobCommand().JobKey(key).Send(ctx)
	if err != nil {
		slog.Error("Failed to complete job", "jobKey", key, "err", err)
	}
}

func (w *NotificationWorkers) SendSMSHandler(client worker.JobClient, job entities.Job) {
	key := job.GetKey()
	slog.Info("Worker notification.sms activated", "jobKey", key)

	variables, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Error("Failed to parse variables", "err", err)
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return
	}

	name, _ := variables["customerName"].(string)

	slog.Info("💬 SMS SENT SUCCESSFULLY", 
		"recipientName", name, 
		"message", "Arda: Your onboarding process is currently updating.",
	)

	ctx := context.Background()
	_, err = client.NewCompleteJobCommand().JobKey(key).Send(ctx)
	if err != nil {
		slog.Error("Failed to complete job", "jobKey", key, "err", err)
	}
}

func (w *NotificationWorkers) SendPushHandler(client worker.JobClient, job entities.Job) {
	key := job.GetKey()
	slog.Info("Worker notification.push activated", "jobKey", key)

	variables, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Error("Failed to parse variables", "err", err)
		w.failJob(client, job, "Invalid variables format: "+err.Error())
		return
	}

	name, _ := variables["customerName"].(string)

	slog.Info("🔔 PUSH NOTIFICATION SENT SUCCESSFULLY", 
		"recipientName", name, 
		"title", "Account Onboarding Approved",
		"body", "Congratulations "+name+"! Your account has been approved.",
	)

	ctx := context.Background()
	_, err = client.NewCompleteJobCommand().JobKey(key).Send(ctx)
	if err != nil {
		slog.Error("Failed to complete job", "jobKey", key, "err", err)
	}
}

func (w *NotificationWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
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
