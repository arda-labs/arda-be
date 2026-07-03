package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
	"github.com/camunda/zeebe/clients/go/v8/pkg/zbc"
)

type ZeebeService struct {
	client zbc.Client
}

type WorkflowTask struct {
	JobKey             int64          `json:"jobKey"`
	Type               string         `json:"type"`
	ElementID          string         `json:"elementId"`
	ProcessInstanceKey int64          `json:"processInstanceKey"`
	CaseID             string         `json:"caseId"`
	CaseCode           string         `json:"caseCode"`
	CustomerID         string         `json:"customerId"`
	CustomerName       string         `json:"customerName"`
	CandidateRole      string         `json:"candidateRole"`
	FormKey            string         `json:"formKey"`
	Variables          map[string]any `json:"variables"`
}

func NewZeebeService(addr string) (*ZeebeService, error) {
	client, err := zbc.NewClient(&zbc.ClientConfig{
		GatewayAddress:         addr,
		UsePlaintextConnection: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create zeebe client: %w", err)
	}

	slog.Info("Connected to Zeebe gateway", "addr", addr)
	return &ZeebeService{client: client}, nil
}

func (s *ZeebeService) Close() {
	if s.client != nil {
		_ = s.client.Close()
	}
}

func (s *ZeebeService) DeployWorkflow(ctx context.Context, name string, content []byte) (int64, error) {
	resp, err := s.client.NewDeployResourceCommand().
		AddResource(content, name).
		Send(ctx)
	if err != nil {
		return 0, fmt.Errorf("deploy bpmn: %w", err)
	}

	if len(resp.GetDeployments()) > 0 {
		dep := resp.GetDeployments()[0]
		if process := dep.GetProcess(); process != nil {
			return process.GetProcessDefinitionKey(), nil
		}
	}

	return 0, nil
}

func (s *ZeebeService) StartWorkflow(ctx context.Context, bpmnProcessID string, variables map[string]any) (int64, error) {
	cmd := s.client.NewCreateInstanceCommand().
		BPMNProcessId(bpmnProcessID).
		LatestVersion()

	if len(variables) > 0 {
		cmdWithVars, err := cmd.VariablesFromMap(variables)
		if err != nil {
			return 0, fmt.Errorf("set variables: %w", err)
		}
		cmd = cmdWithVars
	}

	resp, err := cmd.Send(ctx)
	if err != nil {
		return 0, fmt.Errorf("start workflow: %w", err)
	}

	return resp.GetProcessInstanceKey(), nil
}

func (s *ZeebeService) PublishMessage(ctx context.Context, messageName string, correlationKey string, messageID string, variables map[string]any) (int64, error) {
	cmd := s.client.NewPublishMessageCommand().
		MessageName(messageName).
		CorrelationKey(correlationKey)

	if messageID != "" {
		cmd = cmd.MessageId(messageID)
	}

	if len(variables) > 0 {
		cmdWithVars, err := cmd.VariablesFromMap(variables)
		if err != nil {
			return 0, fmt.Errorf("set message variables: %w", err)
		}
		cmd = cmdWithVars
	}

	resp, err := cmd.Send(ctx)
	if err != nil {
		return 0, fmt.Errorf("publish message: %w", err)
	}

	_ = resp // publish message doesn't return key directly
	return 0, nil
}

func (s *ZeebeService) CancelWorkflow(ctx context.Context, processInstanceKey int64) error {
	_, err := s.client.NewCancelInstanceCommand().
		ProcessInstanceKey(processInstanceKey).
		Send(ctx)
	if err != nil {
		return fmt.Errorf("cancel workflow instance %d: %w", processInstanceKey, err)
	}
	return nil
}

func (s *ZeebeService) ActivateTasks(ctx context.Context, jobType, workerName string, maxJobs int32) ([]WorkflowTask, error) {
	if maxJobs <= 0 || maxJobs > 20 {
		maxJobs = 10
	}
	jobs, err := s.client.NewActivateJobsCommand().
		JobType(jobType).
		MaxJobsToActivate(maxJobs).
		WorkerName(workerName).
		Timeout(30*time.Minute).
		FetchVariables("caseId", "caseCode", "customerId", "customerName", "riskLevel", "reviewDecision", "riskDecision").
		Send(ctx)
	if err != nil {
		return nil, fmt.Errorf("activate jobs: %w", err)
	}

	tasks := make([]WorkflowTask, 0, len(jobs))
	for _, job := range jobs {
		variables, _ := job.GetVariablesAsMap()
		headers, _ := job.GetCustomHeadersAsMap()
		tasks = append(tasks, WorkflowTask{
			JobKey:             job.GetKey(),
			Type:               job.GetType(),
			ElementID:          job.GetElementId(),
			ProcessInstanceKey: job.GetProcessInstanceKey(),
			CaseID:             strVar(variables, "caseId"),
			CaseCode:           strVar(variables, "caseCode"),
			CustomerID:         strVar(variables, "customerId"),
			CustomerName:       strVar(variables, "customerName"),
			CandidateRole:      headers["candidateRole"],
			FormKey:            headers["formKey"],
			Variables:          variables,
		})
	}
	return tasks, nil
}

func (s *ZeebeService) CompleteTask(ctx context.Context, jobKey int64, variables map[string]any) error {
	cmd := s.client.NewCompleteJobCommand().JobKey(jobKey)
	if len(variables) > 0 {
		withVars, err := cmd.VariablesFromMap(variables)
		if err != nil {
			return fmt.Errorf("set variables: %w", err)
		}
		_, err = withVars.Send(ctx)
		return err
	}
	_, err := cmd.Send(ctx)
	return err
}

func (s *ZeebeService) NewJobWorker(jobType string, handler func(client worker.JobClient, job entities.Job)) worker.JobWorker {
	return s.client.NewJobWorker().
		JobType(jobType).
		Handler(handler).
		Open()
}

func strVar(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	if raw, ok := values[key].(json.Number); ok {
		return raw.String()
	}
	return ""
}
